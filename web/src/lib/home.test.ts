import { describe, expect, it } from 'vitest';
import type { Account, Claim, Game, Overview, Runner } from '$lib/api/types';
import accountsFx from '$lib/fixtures/accounts.json';
import claims from '$lib/fixtures/claims.json';
import games from '$lib/fixtures/games.json';
import first from '$lib/fixtures/overview.first_run.json';
import mining from '$lib/fixtures/overview.mining.json';
import nothing from '$lib/fixtures/overview.nothing_live.json';
import many from '$lib/fixtures/runners.many.json';
import runnersMining from '$lib/fixtures/runners.mining.json';
import {
	cardCampaign,
	foldRows,
	gameCards,
	groupClaims,
	homeView,
	fitCount,
	normName,
	notMining,
	visibleCards,
	type GameCard
} from './home';

const G = () => structuredClone(games.games) as Game[];
const R = () => structuredClone(runnersMining) as Runner[];
const A = () => structuredClone(accountsFx) as Account[];

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
	it('cards hold only accounts mining their game', () => {
		const c = gameCards(G(), R());
		expect(c.map((x) => x.rows.length)).toEqual([2, 0, 0, 0]);
		expect(c[0].rows.map((x) => x.runner.account_id)).not.toContain('acc-kick');
	});
	it('rows sorted most advanced first', () => {
		const c = gameCards(G(), R());
		expect(c[0].rows.map((x) => x.runner.account_name)).toEqual(['hJune', 'grub_alt']);
		expect(c[0].rows[0].progress).toBeCloseTo(42 / 60);
	});
	it("moreInUpNext counts that game's queued drops", () => {
		const c = gameCards(G(), R());
		expect(c[0].moreInUpNext).toBe(1);
		expect(c[1].moreInUpNext).toBe(1);
		expect(c[1].queued.map((t) => t.game)).toEqual(['Marvel Rivals']);
		expect(c[1].waitFor).toEqual({ kind: 'account', account: 'hJune' });
		expect(c[3].moreInUpNext).toBe(0);
	});
	it('an idle account with the drop queued waits for a live channel', () => {
		const r = R();
		r[0].current = null;
		r[0].state = 'idle';
		const t = r[0].up_next.find((x) => x.game === 'Marvel Rivals')!;
		t.channels = ['oilrats', 'a', 'b'];
		t.channel_count = 5;
		const c = gameCards(G(), r);
		expect(c[1].waitFor).toEqual({ kind: 'live', channels: ['oilrats', 'a', 'b'], more: 2 });
	});
	it('a busy account waits for the account even if another is signed out', () => {
		const r = R();
		r[2].up_next = [structuredClone(r[0].up_next[1])];
		const c = gameCards(G(), r);
		expect(c[1].waitFor).toEqual({ kind: 'account', account: 'hJune' });
	});
	it('nothing queued waits for nothing', () => {
		const c = gameCards(G(), R());
		expect(c[3].waitFor).toBeNull();
	});
	it('same drop queued on several accounts counts once', () => {
		const c = gameCards(G(), structuredClone(many) as Runner[]);
		expect(c[0].moreInUpNext).toBe(1);
	});
	it('needs_login with a stale current is on no card', () => {
		const r = R();
		r[2].current = structuredClone(r[1].current);
		r[2].current!.game = 'Marvel Rivals';
		const c = gameCards(G(), r);
		expect(c.flatMap((x) => x.rows.map((y) => y.runner.account_name))).not.toContain('kick_main');
	});
	it('rows matched by slug', () => {
		const r = R();
		r[1].current!.game = 'marvel-rivals';
		const c = gameCards(G(), r);
		expect(c[1].rows.map((x) => x.runner.account_name)).toContain('grub_alt');
	});
	it('a paused account is on no card and queues nothing', () => {
		const c = gameCards(G(), R(), new Set(['acc-hjune']));
		expect(c.flatMap((x) => x.rows.map((y) => y.runner.account_id))).not.toContain('acc-hjune');
		expect(c[1].queued).toHaveLength(0);
	});
	it('idle accounts are on no card', () => {
		const c = gameCards(G(), structuredClone(many) as Runner[]);
		expect(c[0].rows).toHaveLength(8);
		expect(c.flatMap((x) => x.rows).every((x) => x.runner.current !== null)).toBe(true);
	});
});

describe('foldRows', () => {
	it('folds 8 mining rows to 3', () => {
		const rows = gameCards(G(), structuredClone(many) as Runner[])[0].rows;
		const f = foldRows(rows);
		expect(f.shown).toHaveLength(3);
		expect(f.hidden).toBe(5);
		expect(f.shown[0].progress).toBeGreaterThan(f.shown[2].progress);
	});
	it('few rows hide nothing', () => {
		expect(foldRows(gameCards(G(), R())[0].rows).hidden).toBe(0);
	});
});

describe('notMining', () => {
	const ids = (a: Account[], r: Runner[]) => notMining(a, r).map((x) => `${x.id}:${x.state}`);
	it('lists accounts no card holds', () => {
		expect(ids(A(), R())).toEqual(['acc-kick:signed_out']);
	});
	it('orders idle, then signed out, then paused', () => {
		const a = A();
		const r = R();
		a[0].enabled = false;
		r[1].current = null;
		r[1].state = 'idle';
		expect(ids(a, r)).toEqual(['acc-alt:idle', 'acc-kick:signed_out', 'acc-hjune:paused']);
	});
	it('keeps account order within a state', () => {
		const a = A();
		const r = R();
		for (const x of r) {
			x.current = null;
			x.state = 'idle';
		}
		a[2].needs_login = false;
		expect(ids(a, r)).toEqual(['acc-hjune:idle', 'acc-alt:idle', 'acc-kick:idle']);
	});
	it('an account without a runner is idle', () => {
		expect(ids(A(), R().slice(0, 2))).toEqual(['acc-kick:signed_out']);
		expect(ids(A(), [])).toEqual(['acc-hjune:idle', 'acc-alt:idle', 'acc-kick:signed_out']);
	});
	it('a paused account mining a stale drop is listed as paused', () => {
		const a = A();
		a[1].enabled = false;
		expect(ids(a, R())).toEqual(['acc-kick:signed_out', 'acc-alt:paused']);
	});
	it('leaves out force watching accounts', () => {
		const r = R();
		r[0].current = null;
		r[0].state = 'force_watching';
		expect(ids(A(), r)).toEqual(['acc-kick:signed_out']);
	});
	it('is empty when every account mines', () => {
		const a = A().slice(0, 2);
		expect(notMining(a, R())).toEqual([]);
	});
});

describe('cardCampaign', () => {
	it('names the campaign an account mines', () => {
		const r = R();
		r[0].current!.campaign_id = 'camp-rust-wk2';
		r[0].current!.campaign_name = 'Rust Week 2';
		expect(cardCampaign(gameCards(G(), r)[0])).toEqual({ platform: 'twitch', id: 'camp-rust-wk2', name: 'Rust Week 2' });
	});
	it("falls back to the game's first campaign", () => {
		const c = gameCards(G(), R()).find((x) => x.game.name === 'Apex Legends')!;
		expect(cardCampaign(c)).toEqual({ platform: 'kick', id: 'camp-apex-kick', name: 'Apex Legends Drops' });
	});
	it('a game with no campaign names none', () => {
		const c = gameCards(G(), R()).find((x) => x.game.name === 'Delta Force')!;
		expect(cardCampaign(c)).toBeNull();
	});
});

describe('homeView', () => {
	const O = (x: unknown) => structuredClone(x) as Overview;
	const base = { games: G(), overviewFailed: false, gamesFailed: false, unreachable: false };
	const fourAccounts = (state: Overview['status']['state']): Overview => {
		const o = O(mining);
		o.status = { ...o.status, state, accounts_total: 4 };
		o.problems = [];
		return o;
	};

	it('loading before the first overview', () => {
		expect(homeView({ ...base, overview: null, games: null })).toBe('loading');
	});
	it('a failed overview load is an error with retry, not a blank page', () => {
		expect(homeView({ ...base, overview: null, games: null, overviewFailed: true })).toBe('error');
	});
	it('a failed games load is an error once the overview is in', () => {
		expect(homeView({ ...base, overview: O(mining), games: null, gamesFailed: true })).toBe('error');
		expect(homeView({ ...base, overview: O(mining), games: null })).toBe('loading');
	});
	it('offline before any data defers to the header alert', () => {
		expect(homeView({ ...base, overview: null, games: null, overviewFailed: true, unreachable: true })).toBe('offline');
	});
	it('no accounts is first run, with or without games', () => {
		expect(homeView({ ...base, overview: O(first), games: [] })).toBe('first_run');
		expect(homeView({ ...base, overview: O(first) })).toBe('first_run');
	});
	it('4 accounts and 0 games shows no games chosen', () => {
		expect(homeView({ ...base, overview: fourAccounts('no_games'), games: [] })).toBe('no_games');
		expect(homeView({ ...base, overview: fourAccounts('no_games'), games: null })).toBe('no_games');
	});
	it('an empty games list wins over a stale overview state', () => {
		expect(homeView({ ...base, overview: fourAccounts('mining'), games: [] })).toBe('no_games');
	});
	it('no games chosen whatever the account state', () => {
		const o = fourAccounts('no_games');
		o.problems = [1, 2, 3, 4].map((i) => ({ kind: 'needs_login' as const, account_id: `a${i}`, account_name: `a${i}` }));
		expect(homeView({ ...base, overview: o, games: [] })).toBe('no_games');
	});
	it('every account signed out', () => {
		const o = fourAccounts('idle');
		o.problems = [1, 2, 3, 4].map((i) => ({ kind: 'needs_login' as const, account_id: `a${i}`, account_name: `a${i}` }));
		expect(homeView({ ...base, overview: o })).toBe('signed_out');
		o.problems.pop();
		expect(homeView({ ...base, overview: o })).toBe('healthy');
	});
	it('every account paused', () => {
		const o = fourAccounts('idle');
		o.status.accounts_paused = 4;
		expect(homeView({ ...base, overview: o })).toBe('paused');
		o.problems = [{ kind: 'needs_login', account_id: 'a1', account_name: 'a1' }];
		expect(homeView({ ...base, overview: o })).toBe('paused');
		o.problems = [1, 2, 3, 4].map((i) => ({ kind: 'needs_login' as const, account_id: `a${i}`, account_name: `a${i}` }));
		expect(homeView({ ...base, overview: o })).toBe('signed_out');
		o.status.accounts_paused = 3;
		o.problems = [];
		expect(homeView({ ...base, overview: o })).toBe('healthy');
	});
	it('no games beats every account paused', () => {
		const o = fourAccounts('no_games');
		o.status.accounts_paused = 4;
		expect(homeView({ ...base, overview: o, games: [] })).toBe('no_games');
	});
	it('games chosen but no campaigns is nothing live', () => {
		expect(homeView({ ...base, overview: O(nothing) })).toBe('nothing_live');
		const g = G().map((x) => ({ ...x, state: 'no_campaign' as const }));
		expect(homeView({ ...base, overview: fourAccounts('idle'), games: g })).toBe('nothing_live');
	});
	it('no games with force watch channels keeps the dashboard', () => {
		const o = fourAccounts('idle');
		o.force_watch = { live: [], more: 0, total: 2 };
		expect(homeView({ ...base, overview: o, games: [] })).not.toBe('no_games');
		expect(homeView({ ...base, overview: o, games: [] })).toBe('nothing_live');
		o.force_watch = null;
		expect(homeView({ ...base, overview: o, games: [] })).toBe('no_games');
	});
	it('no games with force channels nobody can watch is no games', () => {
		// The server reports no_games when no channel has an enabled account.
		const o = fourAccounts('no_games');
		o.force_watch = { live: [], more: 0, total: 2 };
		expect(homeView({ ...base, overview: o, games: [] })).toBe('no_games');
	});
	it('no games while an account force watches keeps the dashboard', () => {
		const o = fourAccounts('no_games');
		o.force_watch = null;
		expect(homeView({ ...base, overview: o, games: [], forceWatching: true })).not.toBe('no_games');
		expect(homeView({ ...base, overview: o, games: [], forceWatching: false })).toBe('no_games');
	});
	it('mining is healthy', () => {
		expect(homeView({ ...base, overview: O(mining) })).toBe('healthy');
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

describe('homeView signed out count', () => {
	const base = { games: G(), overviewFailed: false, gamesFailed: false, unreachable: false };
	const o2 = () => {
		const o = structuredClone(mining) as unknown as Overview;
		o.status.accounts_total = 2;
		o.status.accounts_paused = 0;
		o.status.state = 'mining' as Overview['status']['state'];
		return o;
	};
	it('another problem kind does not count as signed out', () => {
		const o = o2();
		o.problems = [
			{ kind: 'needs_login', account_id: 'a1', account_name: 'a1' },
			{ kind: 'other', account_id: 'a2', account_name: 'a2' }
		] as unknown as Overview['problems'];
		expect(homeView({ ...base, overview: o })).not.toBe('signed_out');
	});
	it('one account counts once however many problems it has', () => {
		const o = o2();
		o.problems = [
			{ kind: 'needs_login', account_id: 'a1', account_name: 'a1' },
			{ kind: 'needs_login', account_id: 'a1', account_name: 'a1' }
		];
		expect(homeView({ ...base, overview: o })).not.toBe('signed_out');
	});
});

describe('visibleCards', () => {
	const mk = (n: number, active: number[] = []): GameCard[] =>
		Array.from({ length: n }, (_, i) => ({
			game: { id: String(i + 1) } as unknown as Game,
			rank: i + 1,
			pos: i + 1,
			size: 'sm' as const,
			rows: active.includes(i + 1)
				? [{ runner: { current: { game: 'x' } } as Runner, progress: 0 }]
				: [],
			queued: [],
			waitFor: null,
			moreInUpNext: 0
		}));
	it('shows all when within the cap', () => {
		const r = visibleCards(mk(7));
		expect(r.shown.length).toBe(7);
		expect(r.hidden).toBe(0);
	});
	it('takes the first 7 by rank when idle', () => {
		const r = visibleCards(mk(10));
		expect(r.shown.map((c) => c.rank)).toEqual([1, 2, 3, 4, 5, 6, 7]);
		expect(r.hidden).toBe(3);
	});
	it('lists a waiting game with no queue before idle games', () => {
		const c = mk(3);
		c[2].game = { id: '3', state: 'waiting' } as unknown as Game;
		expect(visibleCards(c).shown.map((x) => x.rank)).toEqual([3, 1, 2]);
	});
	it('sizes by display position', () => {
		const r = visibleCards(mk(4, [4]));
		expect(r.shown.map((c) => c.rank)).toEqual([4, 1, 2, 3]);
		expect(r.shown.map((c) => c.pos)).toEqual([1, 2, 3, 4]);
		expect(r.shown.map((c) => c.size)).toEqual(['lg', 'lg', 'sm', 'sm']);
	});
	it('keeps a mining low-priority game and lists it first', () => {
		const r = visibleCards(mk(10, [9]));
		expect(r.shown.map((c) => c.rank)).toEqual([9, 1, 2, 3, 4, 5, 6]);
		expect(r.hidden).toBe(3);
	});
	it('lists a mining game above a higher-ranked waiting game', () => {
		const c = mk(3, [3]);
		c[0].game = { id: '1', state: 'waiting' } as unknown as Game;
		c[2].game = { id: '3', state: 'mining' } as unknown as Game;
		expect(visibleCards(c).shown.map((x) => x.rank)).toEqual([3, 1, 2]);
	});
	it('active cards come before idle ones within the cap', () => {
		const cards = mk(5, [4]);
		cards[4].queued = [{ benefit_id: 'b' } as unknown as GameCard['queued'][number]];
		const r = visibleCards(cards);
		expect(r.shown.map((c) => c.rank)).toEqual([4, 5, 1, 2, 3]);
		expect(r.hidden).toBe(0);
	});
});

describe('fitCount', () => {
	const h = [200, 200, 100, 100, 100];
	it('counts cards whose heights and gaps fit', () => {
		expect(fitCount(h, 10, 519, 2)).toBe(2);
		expect(fitCount(h, 10, 520, 2)).toBe(3);
		expect(fitCount(h, 10, 740, 2)).toBe(5);
	});
	it('keeps the minimum when nothing fits', () => {
		expect(fitCount(h, 10, 50, 2)).toBe(2);
		expect(fitCount(h, 10, 0, 2)).toBe(2);
	});
	it('caps at the card count', () => {
		expect(fitCount([100], 10, 9999, 2)).toBe(1);
		expect(fitCount([], 10, 9999, 2)).toBe(0);
	});
});
