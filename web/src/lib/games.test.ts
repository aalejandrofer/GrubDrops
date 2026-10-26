import { beforeEach, describe, expect, it } from 'vitest';
import type { Game, SearchHit } from '$lib/api/types';
import { setLang } from '$lib/i18n/index.svelte';
import { ApiError } from '$lib/api/client';
import {
	campaignLine,
	commitGames,
	dupePriority,
	linkHref,
	linkNames,
	matchParts,
	moveGame,
	moveTarget,
	suggestions,
	withoutId
} from './games';

beforeEach(() => setLang('en'));

const hit = (name: string): SearchHit => ({ id: name, name, chosen: false, priority: null });
const game = (id: string): Game => ({
	id,
	name: id,
	slug: id,
	priority: 1,
	box_art_url: '',
	state: 'no_campaign',
	campaigns: [],
	needs_link: []
});

describe('linkHref', () => {
	it('keeps http and https URLs only', () => {
		expect(linkHref('https://auth.riotgames.com/authorize?x=1')).toBe('https://auth.riotgames.com/authorize?x=1');
		expect(linkHref('http://example.com/link')).toBe('http://example.com/link');
		expect(linkHref('javascript:alert(1)')).toBe('');
		expect(linkHref('/drops')).toBe('');
		expect(linkHref('')).toBe('');
		expect(linkHref(null)).toBe('');
	});
});

describe('linkNames', () => {
	it('lists distinct account names in order', () => {
		const g = game('valo');
		g.needs_link = [
			{ account_id: 'a1', account_name: 'kicker', platform: 'kick', url: '' },
			{ account_id: 'a2', account_name: 'twitchy', platform: 'twitch', url: '' },
			{ account_id: 'a3', account_name: 'kicker', platform: 'twitch', url: '' }
		];
		expect(linkNames(g)).toEqual(['kicker', 'twitchy']);
		expect(linkNames(game('x'))).toEqual([]);
	});
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

describe('moveTarget', () => {
	it('steps one without shift', () => {
		expect(moveTarget(4, 2, -1, false)).toBe(1);
		expect(moveTarget(4, 2, 1, false)).toBe(3);
	});
	it('shift up goes to top', () => expect(moveTarget(4, 2, -1, true)).toBe(0));
	it('shift down goes to bottom', () => expect(moveTarget(4, 1, 1, true)).toBe(3));
	it('already first stays', () => expect(moveTarget(4, 0, -1, true)).toBe(0));
	it('already last stays', () => expect(moveTarget(4, 3, 1, true)).toBe(3));
	it('shift up moves item to the top of the list', () => {
		const l = [game('a'), game('b'), game('c')];
		expect(moveGame(l, 2, moveTarget(3, 2, -1, true)).map((g) => g.id)).toEqual(['c', 'a', 'b']);
	});
	it('shift down moves item to the bottom of the list', () => {
		const l = [game('a'), game('b'), game('c')];
		expect(moveGame(l, 0, moveTarget(3, 0, 1, true)).map((g) => g.id)).toEqual(['b', 'c', 'a']);
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

describe('matchParts', () => {
	it('splits on the first case-insensitive match', () => {
		expect(matchParts('Dead by Daylight', 'dead')).toEqual({ pre: '', hit: 'Dead', post: ' by Daylight' });
		expect(matchParts('Apex Legends', ' leg ')).toEqual({ pre: 'Apex ', hit: 'Leg', post: 'ends' });
	});
	it('no match or empty query leaves the name whole', () => {
		expect(matchParts('Rust', 'tarkov')).toEqual({ pre: 'Rust', hit: '', post: '' });
		expect(matchParts('Rust', '  ')).toEqual({ pre: 'Rust', hit: '', post: '' });
	});
	it('no highlight when lowercasing changes the length', () => {
		expect(matchParts('İstanbul Rust', 'rust')).toEqual({ pre: 'İstanbul Rust', hit: '', post: '' });
	});
});

describe('dupePriority', () => {
	it('reads the priority from a 409', () => {
		expect(dupePriority(new ApiError(409, { error: 'exists', priority: 2 }))).toBe(2);
	});
	it('other failures are null', () => {
		expect(dupePriority(new ApiError(500, { error: 'internal' }))).toBeNull();
		expect(dupePriority(new ApiError(409, { error: 'exists' }))).toBeNull();
		expect(dupePriority(new Error('net'))).toBeNull();
	});
});
