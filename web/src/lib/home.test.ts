import { describe, expect, it } from 'vitest';
import type { Claim, Game, Overview, Runner } from '$lib/api/types';
import claims from '$lib/fixtures/claims.json';
import games from '$lib/fixtures/games.json';
import first from '$lib/fixtures/overview.first_run.json';
import mining from '$lib/fixtures/overview.mining.json';
import nothing from '$lib/fixtures/overview.nothing_live.json';
import many from '$lib/fixtures/runners.many.json';
import runnersMining from '$lib/fixtures/runners.mining.json';
import { foldRows, gameCards, groupClaims, homeState, normName } from './home';

const G = () => structuredClone(games.games) as Game[];
const R = () => structuredClone(runnersMining) as Runner[];

describe('normName', () => {
	it('trims, lowercases, collapses spaces', () => {
		expect(normName('  Marvel   Rivals ')).toBe('marvel rivals');
	});
});

describe('gameCards', () => {
	it('ranks and sizes', () => {
		const c = gameCards(G(), R());
		expect(c.map((x) => x.rank)).toEqual([1, 2, 3, 4]);
		expect(c.map((x) => x.size)).toEqual(['lg', 'lg', 'sm', 'sm']);
	});
	it('orders by priority', () => {
		const g = G().reverse();
		expect(gameCards(g, R())[0].game.name).toBe('Rust');
	});
	it('rows matched by normalised name', () => {
		const r = R();
		r[0].current!.game = ' rust ';
		const c = gameCards(G(), r);
		expect(c[0].rows.map((x) => x.runner.account_name)).toContain('hJune');
	});
	it('needs_login muted on card 1 only', () => {
		const c = gameCards(G(), R());
		const muted = c.map((x) => x.rows.filter((y) => y.muted).length);
		expect(muted).toEqual([1, 0, 0, 0]);
	});
	it('rows sorted most advanced first', () => {
		const c = gameCards(G(), R());
		const live = c[0].rows.filter((x) => !x.muted);
		expect(live.map((x) => x.runner.account_name)).toEqual(['hJune', 'grub_alt']);
		expect(live[0].progress).toBeCloseTo(42 / 60);
		expect(c[0].rows[c[0].rows.length - 1].muted).toBe(true);
	});
	it("moreInUpNext counts that game's queued drops", () => {
		const c = gameCards(G(), R());
		expect(c[0].moreInUpNext).toBe(1);
		expect(c[1].moreInUpNext).toBe(1);
		expect(c[1].queued.map((t) => t.game)).toEqual(['Marvel Rivals']);
		expect(c[1].queuedBy).toBe('hJune');
		expect(c[3].moreInUpNext).toBe(0);
	});
	it('same drop queued on several accounts counts once', () => {
		const c = gameCards(G(), structuredClone(many) as Runner[]);
		expect(c[0].moreInUpNext).toBe(1);
	});
	it('needs_login with a stale current lands on card 1 only', () => {
		const r = R();
		r[2].current = structuredClone(r[1].current);
		r[2].current!.game = 'Marvel Rivals';
		const c = gameCards(G(), r);
		expect(c[1].rows.map((x) => x.runner.account_name)).not.toContain('kick_main');
		expect(c[0].rows.filter((x) => x.muted)).toHaveLength(1);
	});
	it('card 1 lists idle accounts', () => {
		const c = gameCards(G(), structuredClone(many) as Runner[]);
		expect(c[0].rows).toHaveLength(10);
	});
});

describe('foldRows', () => {
	it('folds 10 runners to 3', () => {
		const rows = gameCards(G(), structuredClone(many) as Runner[])[0].rows;
		const f = foldRows(rows);
		expect(f.shown).toHaveLength(3);
		expect(f.hidden).toBe(7);
		expect(f.mining).toBe(8);
		expect(f.idle).toBe(2);
		expect(f.shown[0].progress).toBeGreaterThan(f.shown[2].progress);
	});
	it('few rows hide nothing', () => {
		expect(foldRows(gameCards(G(), R())[0].rows).hidden).toBe(0);
	});
});

describe('homeState', () => {
	it('maps each case', () => {
		expect(homeState(first as Overview, false)).toBe('first_run');
		expect(homeState({ ...(first as Overview), status: { ...first.status, state: 'no_games' as const } }, false)).toBe('first_run');
		expect(homeState(mining as Overview, false)).toBe('healthy');
		expect(homeState(nothing as Overview, false)).toBe('nothing_live');
		expect(homeState(mining as Overview, true)).toBe('offline');
		expect(homeState(null, true)).toBe('offline');
	});
});

describe('groupClaims', () => {
	const now = Date.parse('2026-10-25T12:00:00Z');
	const items = claims.items as Claim[];
	it('buckets in the settings zone', () => {
		const g = groupClaims(items, now, 'UTC');
		expect(g.today.length + g.yesterday.length + g.earlier.length).toBe(items.length);
		expect(g.today.every((x) => x.claimed_at.startsWith('2026-10-25'))).toBe(true);
	});
	it('zone moves the day boundary', () => {
		const c = [{ ...items[0], claimed_at: '2026-10-25T23:30:00Z' }];
		expect(groupClaims(c, now, 'UTC').today).toHaveLength(1);
		expect(groupClaims(c, now, 'Asia/Tokyo').today).toHaveLength(0);
	});
});
