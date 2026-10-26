import { beforeEach, describe, expect, it } from 'vitest';
import type { Game, SearchHit } from '$lib/api/types';
import { setLang } from '$lib/i18n/index.svelte';
import { campaignLine, commitGames, moveGame, suggestions, withoutId } from './games';

beforeEach(() => setLang('en'));

const hit = (name: string): SearchHit => ({ id: name, name, chosen: false, priority: null });
const game = (id: string): Game => ({
	id,
	name: id,
	slug: id,
	priority: 1,
	box_art_url: '',
	state: 'no_campaign',
	campaigns: []
});

describe('withoutId', () => {
	it('drops one id and keeps order', () => {
		expect(withoutId(['a', 'b', 'c'], 'b')).toEqual(['a', 'c']);
		expect(withoutId(['a'], 'z')).toEqual(['a']);
	});
});

describe('suggestions', () => {
	it('adds free text when no exact hit', () => {
		expect(suggestions([hit('Rust')], 'Rus')).toEqual([
			{ kind: 'hit', hit: hit('Rust') },
			{ kind: 'add', name: 'Rus' }
		]);
		expect(suggestions([hit('Rust')], ' rust ')).toEqual([{ kind: 'hit', hit: hit('Rust') }]);
		expect(suggestions([], '')).toEqual([]);
		expect(suggestions([], '   ')).toEqual([]);
	});
});

describe('campaignLine', () => {
	const c = { id: 'c', platform: 'twitch' as const, name: 'n', drops: 3, ends_at: '2026-10-31T12:00:00Z' };
	it('formats drops and end date', () => {
		expect(campaignLine(c, 'UTC')).toBe('3 drops · ends Oct 31');
		expect(campaignLine({ ...c, drops: 1 }, 'UTC')).toBe('1 drop · ends Oct 31');
		expect(campaignLine({ ...c, ends_at: null }, 'UTC')).toBe('3 drops');
	});
});

describe('moveGame', () => {
	it('moves', () => {
		const l = [game('a'), game('b'), game('c')];
		expect(moveGame(l, 2, 0).map((g) => g.id)).toEqual(['c', 'a', 'b']);
	});
});

describe('commitGames', () => {
	function io(over: Partial<{ save: () => Promise<void>; fetch: () => Promise<Game[]> }> = {}) {
		let cur = [game('a'), game('b')];
		const saved: string[][] = [];
		return {
			saved,
			cur: () => cur,
			io: {
				get: () => cur,
				set: (g: Game[]) => (cur = g),
				setGames: async (ids: string[]) => {
					saved.push(ids);
					await over.save?.();
				},
				fetchGames: over.fetch ?? (async () => cur)
			}
		};
	}
	it('saves the full order', async () => {
		const x = io();
		expect(await commitGames(x.io, [game('b'), game('a')])).toBe(true);
		expect(x.saved).toEqual([['b', 'a']]);
	});
	it('skips an unchanged order', async () => {
		const x = io();
		expect(await commitGames(x.io, [game('a'), game('b')])).toBe(true);
		expect(x.saved).toEqual([]);
	});
	it('takes the server list on failure', async () => {
		const server = [game('a'), game('b')];
		const x = io({ save: () => Promise.reject(new Error('x')), fetch: async () => server });
		expect(await commitGames(x.io, [game('b'), game('a')])).toBe(false);
		expect(x.cur()).toBe(server);
	});
	it('restores the old list when failure and refetch both fail', async () => {
		const x = io({ save: () => Promise.reject(new Error('x')), fetch: () => Promise.reject(new Error('y')) });
		const before = x.cur();
		expect(await commitGames(x.io, [game('b')])).toBe(false);
		expect(x.cur()).toBe(before);
	});
});
