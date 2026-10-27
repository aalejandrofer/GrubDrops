import { describe, expect, it } from 'vitest';
import type { ForceChannel, Runner } from '$lib/api/types';
import { knownIds, moveChannel, parseChannel, toPut, togglePick, watchingIds } from './force-watch';

const ch = (channel: string, platform: 'twitch' | 'kick' = 'twitch', accounts: string[] = []): ForceChannel => ({
	platform,
	channel,
	live: null,
	accounts,
	watching: []
});

describe('parseChannel', () => {
	it('reads twitch and kick links', () => {
		expect(parseChannel('https://www.twitch.tv/keshaeuw')).toEqual({ platform: 'twitch', name: 'keshaeuw' });
		expect(parseChannel('kick.com/XQC')).toEqual({ platform: 'kick', name: 'xqc' });
		expect(parseChannel('  m.twitch.tv/thebausffs/videos ')).toEqual({ platform: 'twitch', name: 'thebausffs' });
	});
	it('rejects short names, other sites and empty input', () => {
		expect(parseChannel('m.twitch.tv/a')).toBeNull();
		expect(parseChannel('youtube.com/x')).toBeNull();
		expect(parseChannel('youtube.com/somebody')).toBeNull();
		expect(parseChannel('')).toBeNull();
		expect(parseChannel('   ')).toBeNull();
		expect(parseChannel('https://twitch.tv/')).toBeNull();
	});
	it('takes a hyphen on kick only, like the server', () => {
		expect(parseChannel('kick.com/some-one')).toEqual({ platform: 'kick', name: 'some-one' });
		expect(parseChannel('twitch.tv/some-one')).toBeNull();
	});
});

describe('moveChannel', () => {
	const list = [ch('a1'), ch('b1'), ch('c1')];
	it('swaps with the neighbour', () => {
		expect(moveChannel(list, 0, 1).map((c) => c.channel)).toEqual(['b1', 'a1', 'c1']);
		expect(moveChannel(list, 2, -1).map((c) => c.channel)).toEqual(['a1', 'c1', 'b1']);
	});
	it('out of range returns a plain copy', () => {
		const top = moveChannel(list, 0, -1);
		expect(top.map((c) => c.channel)).toEqual(['a1', 'b1', 'c1']);
		expect(top).not.toBe(list);
		expect(moveChannel(list, 2, 1).map((c) => c.channel)).toEqual(['a1', 'b1', 'c1']);
	});
});

describe('toPut', () => {
	it('sends only platform, channel and accounts', () => {
		const c = { ...ch('keshaeuw', 'twitch', ['a1']), live: true, watching: ['a1'] };
		expect(toPut([c])).toEqual([{ platform: 'twitch', channel: 'keshaeuw', accounts: ['a1'] }]);
	});
	it('drops account ids that are no longer known', () => {
		const c = ch('keshaeuw', 'twitch', ['a1', 'gone']);
		expect(toPut([c], new Set(['a1']))).toEqual([{ platform: 'twitch', channel: 'keshaeuw', accounts: ['a1'] }]);
	});
	it('keeps every id when no accounts are known yet', () => {
		const c = ch('keshaeuw', 'twitch', ['a1', 'a2']);
		expect(toPut([c], new Set())[0].accounts).toEqual(['a1', 'a2']);
	});
	it('copies the accounts array', () => {
		const c = ch('keshaeuw', 'twitch', ['a1']);
		expect(toPut([c])[0].accounts).not.toBe(c.accounts);
	});
});

describe('knownIds', () => {
	it('filters to known ids, or keeps all when none are known', () => {
		expect(knownIds(['a', 'gone', 'b'], new Set(['a', 'b']))).toEqual(['a', 'b']);
		expect(knownIds(['a'], new Set())).toEqual(['a']);
	});
});

describe('togglePick', () => {
	it('adds and removes an id', () => {
		expect(togglePick([], 'a')).toEqual(['a']);
		expect(togglePick(['a', 'b'], 'a')).toEqual(['b']);
	});
});

describe('watchingIds', () => {
	const r = (id: string, state: Runner['state'], channel: string, platform: 'twitch' | 'kick' = 'twitch') =>
		({ account_id: id, state, channel, platform }) as Runner;
	it('follows force watching runners on that channel', () => {
		const c = { ...ch('keshaeuw', 'twitch', ['a1', 'a2']), watching: ['a2'] };
		const runners = [r('a1', 'force_watching', 'keshaeuw'), r('a2', 'watching', 'keshaeuw'), r('a3', 'force_watching', 'keshaeuw', 'kick')];
		expect(watchingIds(c, runners)).toEqual(['a1']);
	});
	it('falls back to the server list before runners load', () => {
		const c = { ...ch('keshaeuw'), watching: ['a2'] };
		expect(watchingIds(c, [])).toEqual(['a2']);
	});
});
