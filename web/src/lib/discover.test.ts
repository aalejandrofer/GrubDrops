import { describe, expect, it } from 'vitest';
import type { DiscoverCampaign } from '$lib/api/types';
import { filterGroups, gameSlug, groupByGame, isAdded, minutesLeft } from './discover';

const camp = (p: Partial<DiscoverCampaign> & Pick<DiscoverCampaign, 'id' | 'game'>): DiscoverCampaign => ({
	platform: 'twitch',
	box_art_url: '',
	name: p.id,
	starts_at: null,
	ends_at: null,
	link_url: '',
	needs_link: false,
	rewards: [],
	...p
});

describe('gameSlug', () => {
	it('mirrors Go gameslug.Slug', () => {
		expect(gameSlug('Apex Legends')).toBe('apex-legends');
		expect(gameSlug('Counter-Strike 2')).toBe('counter-strike-2');
		expect(gameSlug("Tom's Game!!")).toBe('toms-game');
		expect(gameSlug('  _Rust_ ')).toBe('rust');
	});
});

describe('isAdded', () => {
	const chosen = [
		{ name: 'Apex Legends', slug: 'apex-legends' },
		{ name: 'Counter Strike', slug: 'counter-strike-2' }
	];
	it('matches by normalized name', () => {
		expect(isAdded(chosen, '  apex   LEGENDS ')).toBe(true);
	});
	it('matches by slug', () => {
		expect(isAdded(chosen, 'Counter-Strike 2')).toBe(true);
	});
	it('misses other games', () => {
		expect(isAdded(chosen, 'Rust')).toBe(false);
		expect(isAdded([], 'Rust')).toBe(false);
	});
});

describe('groupByGame', () => {
	const camps = [
		camp({ id: 'k1', game: 'rust', platform: 'kick', box_art_url: 'kick.jpg', ends_at: '2099-01-03T00:00:00Z' }),
		camp({ id: 't1', game: 'Rust', platform: 'twitch', box_art_url: 'twitch.jpg', ends_at: '2099-01-02T00:00:00Z' }),
		camp({ id: 't2', game: 'Apex Legends', ends_at: null }),
		camp({ id: 't3', game: 'Apex Legends', ends_at: '2099-01-01T00:00:00Z' })
	];
	const groups = groupByGame(camps, [{ name: 'Apex Legends', slug: 'apex-legends' }]);

	it('groups across platforms by slug, games A to Z', () => {
		expect(groups.map((g) => g.name)).toEqual(['Apex Legends', 'Rust']);
		expect(groups[1].platforms).toEqual(['kick', 'twitch']);
	});
	it('prefers Twitch name and box art', () => {
		expect(groups[1].name).toBe('Rust');
		expect(groups[1].box_art_url).toBe('twitch.jpg');
	});
	it('orders campaigns soonest ending first, open-ended last', () => {
		expect(groups[0].campaigns.map((c) => c.id)).toEqual(['t3', 't2']);
		expect(groups[1].campaigns.map((c) => c.id)).toEqual(['t1', 'k1']);
	});
	it('flags chosen games', () => {
		expect(groups.map((g) => g.added)).toEqual([true, false]);
	});
});

describe('filterGroups', () => {
	const groups = groupByGame(
		[
			camp({ id: 'k1', game: 'Rust', platform: 'kick', name: 'Kick Rust', rewards: [{ name: 'Crate', image_url: '', minutes: 60 }] }),
			camp({ id: 't1', game: 'Rust', name: 'Twitch Rust' }),
			camp({ id: 't2', game: 'Apex Legends' })
		],
		[{ name: 'Apex Legends', slug: 'apex-legends' }]
	);
	const all = { q: '', platform: 'all' as const, hideAdded: false };

	it('passes everything with an empty filter', () => {
		expect(filterGroups(groups, all)).toEqual(groups);
	});
	it('keeps only one platform', () => {
		const out = filterGroups(groups, { ...all, platform: 'kick' });
		expect(out.map((g) => g.name)).toEqual(['Rust']);
		expect(out[0].campaigns.map((c) => c.id)).toEqual(['k1']);
		expect(out[0].platforms).toEqual(['kick']);
	});
	it('matches game, campaign and reward names', () => {
		expect(filterGroups(groups, { ...all, q: 'apex' }).map((g) => g.name)).toEqual(['Apex Legends']);
		expect(filterGroups(groups, { ...all, q: 'twitch rust' })[0].campaigns.map((c) => c.id)).toEqual(['t1']);
		expect(filterGroups(groups, { ...all, q: 'CRATE' })[0].campaigns.map((c) => c.id)).toEqual(['k1']);
		expect(filterGroups(groups, { ...all, q: 'nothing' })).toEqual([]);
	});
	it('hides added games', () => {
		expect(filterGroups(groups, { ...all, hideAdded: true }).map((g) => g.name)).toEqual(['Rust']);
	});
});

describe('minutesLeft', () => {
	const now = Date.parse('2099-01-01T00:00:00Z');
	it('counts whole minutes, floored at 0', () => {
		expect(minutesLeft('2099-01-01T01:30:59Z', now)).toBe(90);
		expect(minutesLeft('2098-12-31T00:00:00Z', now)).toBe(0);
	});
	it('is null without an end', () => {
		expect(minutesLeft(null, now)).toBeNull();
		expect(minutesLeft('bad', now)).toBeNull();
	});
});
