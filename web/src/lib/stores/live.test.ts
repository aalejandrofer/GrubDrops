import { describe, expect, it, vi } from 'vitest';
import type { Api } from '$lib/api/client';
import type { Claim, ForceChannel, HealthEvent, Runner } from '$lib/api/types';
import { createLive, upsertBy, type EventSourceLike } from './live.svelte';

class FakeSource implements EventSourceLike {
	onopen: (() => void) | null = null;
	onerror: (() => void) | null = null;
	closed = false;
	handlers = new Map<string, (ev: { data: string }) => void>();
	addEventListener(name: string, fn: (ev: { data: string }) => void) {
		this.handlers.set(name, fn);
	}
	close() {
		this.closed = true;
	}
	emit(name: string, data: unknown) {
		this.handlers.get(name)!({ data: JSON.stringify(data) });
	}
}

const runner = (id: string, minutes = 0) => ({ account_id: id, minutes }) as unknown as Runner;
const claim = (acc: string, ben: string) => ({ account_id: acc, benefit_id: ben }) as unknown as Claim;

function setup(session: () => Promise<{ authed: boolean }> = async () => ({ authed: true })) {
	const api = {
		overview: vi.fn(async () => ({ status: { state: 'idle' } })),
		runners: vi.fn(async () => [] as Runner[]),
		games: vi.fn(async () => ({ games: [], last_scan_at: null })),
		accounts: vi.fn(async () => []),
		claims: vi.fn(async () => ({ total: 0, items: [], games: [], accounts: [] })),
		forceWatch: vi.fn(async () => ({ channels: [] as ForceChannel[] })),
		session: vi.fn(session)
	};
	const sources: FakeSource[] = [];
	const delays: number[] = [];
	const pending: (() => void)[] = [];
	const onUnauthorized = vi.fn();
	const live = createLive({
		api: api as unknown as Api,
		eventSource: () => {
			const s = new FakeSource();
			sources.push(s);
			return s;
		},
		schedule: (fn, ms) => {
			delays.push(ms);
			pending.push(fn);
			return () => {};
		},
		tick: () => () => {},
		now: () => 1234,
		onUnauthorized
	});
	live.start();
	return { api, live, sources, delays, pending, onUnauthorized, cur: () => sources[sources.length - 1] };
}

const settle = () => new Promise((r) => setTimeout(r, 0));

describe('upsertBy', () => {
	it('replaces in place, else prepends', () => {
		const l = [{ k: 'a', v: 1 }, { k: 'b', v: 2 }];
		expect(upsertBy(l, { k: 'b', v: 9 }, (x) => x.k)).toEqual([{ k: 'a', v: 1 }, { k: 'b', v: 9 }]);
		expect(upsertBy(l, { k: 'c', v: 3 }, (x) => x.k)[0].k).toBe('c');
	});
});

describe('live events', () => {
	it('runner event upserts by account_id', () => {
		const { live, cur } = setup();
		cur().emit('runner', runner('a'));
		cur().emit('runner', runner('b'));
		cur().emit('runner', runner('a', 5));
		expect(live.runners.map((r) => r.account_id)).toEqual(['a', 'b']);
		expect(live.runners[0].minutes).toBe(5);
		cur().emit('runner', runner('c'));
		expect(live.runners.map((r) => r.account_id)).toEqual(['a', 'b', 'c']);
		expect(live.updatedAt).toBe(1234);
	});

	it('claim event upserts by account_id + benefit_id', async () => {
		const { live, cur, api } = setup();
		cur().emit('claim', claim('a', 'x'));
		cur().emit('claim', claim('a', 'x'));
		expect(live.recent).toHaveLength(1);
		for (let i = 0; i < 11; i++) cur().emit('claim', claim('b', `d${i}`));
		expect(live.recent).toHaveLength(10);
		expect(live.recent[0].benefit_id).toBe('d10');
		await settle();
		expect(api.overview).toHaveBeenCalled();
	});

	it('games event refetches games and overview', async () => {
		const { api, cur } = setup();
		cur().emit('games', {});
		await settle();
		expect(api.games).toHaveBeenCalledTimes(1);
		expect(api.overview).toHaveBeenCalledTimes(1);
		expect(api.accounts).not.toHaveBeenCalled();
	});

	it('accounts event refetches accounts, runners and overview', async () => {
		const { api, cur } = setup();
		cur().emit('accounts', {});
		await settle();
		expect(api.accounts).toHaveBeenCalledTimes(1);
		expect(api.runners).toHaveBeenCalledTimes(1);
		expect(api.overview).toHaveBeenCalledTimes(1);
		// A removed account leaves force watch on the server; reload the list.
		expect(api.forceWatch).toHaveBeenCalledTimes(1);
		expect(api.games).not.toHaveBeenCalled();
	});

	it('force_watch event refetches the list and overview', async () => {
		const { api, live, cur } = setup();
		const c: ForceChannel = { platform: 'twitch', channel: 'keshaeuw', live: true, accounts: ['a'], watching: [] };
		api.forceWatch.mockResolvedValueOnce({ channels: [c] });
		cur().emit('force_watch', {});
		await settle();
		expect(api.forceWatch).toHaveBeenCalledTimes(1);
		expect(api.overview).toHaveBeenCalledTimes(1);
		expect(api.games).not.toHaveBeenCalled();
		expect(live.forceWatch).toEqual([c]);
	});

	it('a runner entering or leaving force watching refetches overview and the force list', async () => {
		const { api, cur } = setup();
		const r = (id: string, state: Runner['state']) => ({ account_id: id, state }) as Runner;
		const calls = () => [api.overview.mock.calls.length, api.forceWatch.mock.calls.length];
		cur().emit('runner', r('a', 'watching'));
		await settle();
		expect(calls()).toEqual([0, 0]);
		cur().emit('runner', r('a', 'force_watching'));
		await settle();
		expect(calls()).toEqual([1, 1]);
		cur().emit('runner', r('a', 'force_watching'));
		await settle();
		expect(calls()).toEqual([1, 1]);
		cur().emit('runner', r('a', 'idle'));
		await settle();
		expect(calls()).toEqual([2, 2]);
	});

	it('log event prepends, capped at 50', () => {
		const { live, cur } = setup();
		for (let i = 0; i < 55; i++) cur().emit('log', { msg: `m${i}` } as HealthEvent);
		expect(live.log).toHaveLength(50);
		expect(live.log[0].msg).toBe('m54');
		live.seedLog(Array.from({ length: 60 }, (_, i) => ({ msg: `s${i}` }) as HealthEvent));
		expect(live.log).toHaveLength(50);
		expect(live.log[0].msg).toBe('s0');
	});
});

describe('live reconnect', () => {
	it('reconnect backoff 1,2,4,8,16,30', async () => {
		const { live, sources, delays, pending } = setup();
		for (let i = 0; i < 7; i++) {
			sources[sources.length - 1].onerror!();
			await settle();
			if (i === 0) {
				expect(live.unreachable).toBe(true);
				expect(live.retryIn).toBe(1);
			}
			pending.shift()!();
		}
		expect(delays).toEqual([1000, 2000, 4000, 8000, 16000, 30000, 30000]);
		expect(sources).toHaveLength(8);
		expect(sources[0].closed).toBe(true);
	});

	it('a failed overview or games load is flagged until the next success', async () => {
		const { api, live } = setup();
		api.overview.mockRejectedValueOnce(new Error('500'));
		api.games.mockRejectedValueOnce(new Error('500'));
		await live.refetchAll();
		expect(live.overview).toBeNull();
		expect(live.failed).toEqual({ overview: true, games: true });
		await live.refetchAll();
		expect(live.overview).not.toBeNull();
		expect(live.failed).toEqual({ overview: false, games: false });
	});

	it('reconnect refetches everything', async () => {
		const { api, live, sources, pending, cur } = setup();
		cur().onopen!();
		await settle();
		sources[0].onerror!();
		await settle();
		expect(live.unreachable).toBe(true);
		pending.shift()!();
		const before = [api.overview, api.runners, api.games, api.accounts, api.claims, api.forceWatch].map((m) => m.mock.calls.length);
		cur().onopen!();
		await settle();
		const after = [api.overview, api.runners, api.games, api.accounts, api.claims, api.forceWatch].map((m) => m.mock.calls.length);
		expect(after.map((n, i) => n - before[i])).toEqual([1, 1, 1, 1, 1, 1]);
		expect(api.claims).toHaveBeenLastCalledWith({ limit: 10 });
		expect(live.unreachable).toBe(false);
		sources[1].onerror!();
		await settle();
		expect(live.retryIn).toBe(1);
	});

	it('sse error with unauthed session stops reconnecting', async () => {
		const { live, sources, delays, onUnauthorized } = setup(async () => ({ authed: false }));
		sources[0].onerror!();
		await settle();
		expect(onUnauthorized).toHaveBeenCalledTimes(1);
		expect(delays).toEqual([]);
		sources[0].onerror!();
		await settle();
		expect(onUnauthorized).toHaveBeenCalledTimes(1);
		expect(delays).toEqual([]);
		expect(live.unreachable).toBe(false);
	});

	it('stop closes the source and cancels a pending reconnect', async () => {
		const { live, sources, delays } = setup();
		live.stop();
		expect(sources[0].closed).toBe(true);
		sources[0].onerror!();
		await settle();
		expect(delays).toEqual([]);
	});

	it('a double onerror schedules one reconnect', async () => {
		const { cur, delays } = setup();
		const src = cur();
		src.onerror!();
		src.onerror!();
		await settle();
		expect(delays).toHaveLength(1);
	});
});
