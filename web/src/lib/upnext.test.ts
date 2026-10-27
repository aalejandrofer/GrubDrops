import { describe, expect, it } from 'vitest';
import type { Platform, Runner, Target } from '$lib/api/types';
import mining from '$lib/fixtures/runners.mining.json';
import {
	afterLabel,
	anyCustom,
	applyMineNow,
	byGame,
	canMineNow,
	commitGameOrder,
	commitMineNow,
	commitMove,
	foldQueue,
	inGameOrder,
	mineTarget,
	moveItem,
	queues,
	readGameOrder,
	reorderByGame,
	settleGameOrder,
	withoutDrop,
	writeGameOrder,
	GAME_ORDER_KEY,
	type GameGroup,
	type Queue
} from './upnext';

const fx = () => structuredClone(mining) as Runner[];

describe('moveItem', () => {
	it('moves up and down', () => {
		expect(moveItem(['a', 'b', 'c'], 2, 0)).toEqual(['c', 'a', 'b']);
		expect(moveItem(['a', 'b', 'c'], 0, 2)).toEqual(['b', 'c', 'a']);
	});
	it('out of range is a copy', () => {
		const l = ['a', 'b', 'c'];
		const out = moveItem(l, 5, 0);
		expect(out).toEqual(l);
		expect(out).not.toBe(l);
	});
});

describe('queues', () => {
	it('skips runners without up_next', () => {
		const q = queues(fx());
		expect(q).toHaveLength(2);
		expect(q.map((x) => x.accountName)).toEqual(['hJune', 'grub_alt']);
	});
	it('marks signed-out runners as unable to mine now', () => {
		const rs = fx();
		rs[1].state = 'needs_login';
		expect(queues(rs).map((x) => x.canMine)).toEqual([true, false]);
	});
});

describe('canMineNow', () => {
	it('allows running states only', () => {
		for (const s of ['watching', 'sleeping', 'starting', 'awaiting_connect'] as const) expect(canMineNow({ state: s })).toBe(true);
		for (const s of ['needs_login', 'no_games', 'idle'] as const) expect(canMineNow({ state: s })).toBe(false);
	});
});

describe('afterLabel', () => {
	it('item 0 follows the current drop, others the previous item', () => {
		const r = fx()[0];
		expect(afterLabel(r, 0)).toBe(r.current?.benefit_name ?? null);
		expect(afterLabel(r, 1)).toBe(r.up_next[0].benefit_name);
	});
	it('item 0 with no current drop is null', () => {
		const r = { ...fx()[0], current: null };
		expect(afterLabel(r, 0)).toBeNull();
	});
});

describe('anyCustom', () => {
	it('reset hidden when refetched runners have order_custom false', () => {
		const local = fx();
		local[0].order_custom = true;
		expect(anyCustom(local)).toBe(true);
		expect(anyCustom(fx())).toBe(false);
	});
});

describe('commitMove', () => {
	const mk = (setOrder: () => Promise<void>, fetchRunners: () => Promise<Runner[]>) => {
		let cur = fx();
		const calls: string[][] = [];
		return {
			get cur() {
				return cur;
			},
			calls,
			io: {
				get: () => cur,
				set: (r: Runner[]) => (cur = r),
				setOrder: async (_id: string, ids: string[]) => {
					calls.push(ids);
					return setOrder();
				},
				fetchRunners
			}
		};
	};
	const first = fx()[0].up_next.map((x) => x.benefit_id);

	it('moves optimistically, saves, then takes the server list', async () => {
		const server = fx();
		const h = mk(async () => {}, async () => server);
		expect(await commitMove(h.io, 'acc-hjune', 1, 0)).toBe(true);
		expect(h.calls).toEqual([[first[1], first[0]]]);
		expect(h.cur).toBe(server);
	});
	it('failed save takes the server list', async () => {
		const server = fx();
		const h = mk(() => Promise.reject(new Error('x')), async () => server);
		expect(await commitMove(h.io, 'acc-hjune', 1, 0)).toBe(false);
		expect(h.cur).toBe(server);
	});
	it('failed save and failed refetch roll back', async () => {
		const h = mk(() => Promise.reject(new Error('x')), () => Promise.reject(new Error('y')));
		const before = h.cur;
		expect(await commitMove(h.io, 'acc-hjune', 1, 0)).toBe(false);
		expect(h.cur).toBe(before);
	});
	it('a no-op move makes no request', async () => {
		const h = mk(async () => {}, async () => fx());
		await commitMove(h.io, 'acc-hjune', 0, 0);
		expect(h.calls).toEqual([]);
	});
});

describe('foldQueue', () => {
	const list = [1, 2, 3, 4, 5, 6, 7];
	it('shows the first 5 and counts the rest', () => {
		expect(foldQueue(list, false)).toEqual({ rows: [1, 2, 3, 4, 5], more: 2 });
	});
	it('expanded shows every row and keeps the count', () => {
		expect(foldQueue(list, true)).toEqual({ rows: list, more: 2 });
	});
	it('5 or fewer has nothing more', () => {
		expect(foldQueue([1, 2, 3], false)).toEqual({ rows: [1, 2, 3], more: 0 });
	});
	it('a move among visible rows leaves hidden rows in place', () => {
		const moved = moveItem(list, 4, 0);
		expect(moved).toEqual([5, 1, 2, 3, 4, 6, 7]);
		expect(foldQueue(moved, false).rows).toEqual([5, 1, 2, 3, 4]);
	});
});

describe('mineTarget', () => {
	it('is the first queue that can switch', () => {
		const qs = queues(fx());
		expect(mineTarget(qs)?.accountName).toBe('hJune');
		qs[0].canMine = false;
		expect(mineTarget(qs)?.accountName).toBe('grub_alt');
		qs[1].canMine = false;
		expect(mineTarget(qs)).toBeNull();
		expect(mineTarget([])).toBeNull();
	});
});

describe('applyMineNow', () => {
	it('starts item 0 and puts the old current first in Up next', () => {
		const r = fx()[0];
		const [n0, ...rest] = r.up_next;
		const out = applyMineNow(r);
		expect(out.current).toEqual(n0);
		expect(out.minutes).toBe(n0.minutes);
		expect(out.up_next).toEqual([r.current, ...rest]);
		expect(out.order_custom).toBe(true);
	});
	it('with no current, Up next loses item 0 only', () => {
		const r = { ...fx()[0], current: null };
		expect(applyMineNow(r).up_next).toEqual(r.up_next.slice(1));
	});
	it('empty Up next is unchanged', () => {
		const r = { ...fx()[0], up_next: [] };
		expect(applyMineNow(r)).toBe(r);
	});
});

describe('commitMineNow', () => {
	const mk = (mineNow: () => Promise<unknown>, fetchRunners: () => Promise<Runner[]>) => {
		let cur = fx();
		const calls: string[] = [];
		return {
			get cur() {
				return cur;
			},
			calls,
			io: {
				get: () => cur,
				set: (r: Runner[]) => (cur = r),
				mineNow: async (id: string) => {
					calls.push(id);
					return mineNow();
				},
				fetchRunners
			}
		};
	};
	it('applies the switch at once and keeps it on success', async () => {
		const before = fx()[0];
		const h = mk(async () => ({ benefit_id: before.up_next[0].benefit_id }), async () => [] as Runner[]);
		expect(await commitMineNow(h.io, before.account_id)).toBe(true);
		expect(h.calls).toEqual([before.account_id]);
		expect(h.cur[0].current?.benefit_id).toBe(before.up_next[0].benefit_id);
		expect(h.cur[0].up_next[0].benefit_id).toBe(before.current?.benefit_id);
	});
	it('on failure the server list wins', async () => {
		const server = fx();
		const h = mk(() => Promise.reject(new Error('x')), async () => server);
		expect(await commitMineNow(h.io, server[0].account_id)).toBe(false);
		expect(h.cur).toBe(server);
	});
	it('restores the old list when the refetch fails too', async () => {
		const h = mk(
			() => Promise.reject(new Error('x')),
			() => Promise.reject(new Error('y'))
		);
		const before = h.cur;
		expect(await commitMineNow(h.io, before[0].account_id)).toBe(false);
		expect(h.cur).toBe(before);
	});
	it('does nothing for an account without Up next', async () => {
		const h = mk(async () => ({}), async () => [] as Runner[]);
		expect(await commitMineNow(h.io, 'nope')).toBe(false);
		expect(h.calls).toEqual([]);
	});
});

describe('byGame', () => {
	const item = (game: string, campaign: string, benefit: string, req = 60, min = 0, platform: Platform = 'twitch'): Target => ({
		game,
		platform,
		campaign_id: campaign,
		campaign_name: `${campaign} name`,
		benefit_id: benefit,
		benefit_name: benefit,
		image_url: '',
		required_minutes: req,
		minutes: min,
		ends_at: null,
		channels: [],
		channel_count: 0
	});
	const queue = (id: string, items: Target[], platform: Platform = 'twitch'): Queue => ({
		accountId: id,
		accountName: `${id} name`,
		platform,
		items,
		custom: false,
		canMine: true
	});

	it('folds every account into one entry per game in queue order', () => {
		const g = byGame([
			queue('a', [item('Rust', 'r1', 'x'), item('Rust', 'r2', 'y'), item('Apex', 'p1', 'z')]),
			queue('b', [item('Apex', 'p1', 'z'), item('Rust', 'r1', 'x')])
		]);
		expect(g.map((x) => x.game)).toEqual(['Rust', 'Apex']);
		expect(g[0].drops).toBe(3);
		expect(g[0].campaigns.map((c) => [c.id, c.drops.map((d) => d.accountId)])).toEqual([
			['r1', ['a', 'b']],
			['r2', ['a']]
		]);
		expect(g[1].drops).toBe(2);
		expect(g[1].accounts.map((a) => a.id)).toEqual(['a', 'b']);
	});

	it('places a game by its earliest position on any account, ties by runner order', () => {
		const g = byGame([
			queue('a', [item('Rust', 'r1', 'x'), item('SMITE', 's1', 'y')]),
			queue('b', [item('Apex', 'p1', 'z')])
		]);
		expect(g.map((x) => x.game)).toEqual(['Rust', 'Apex', 'SMITE']);
	});

	it('lists accounts in runner order', () => {
		const g = byGame([queue('a', [item('Apex', 'p1', 'w'), item('Rust', 'r1', 'x')]), queue('b', [item('Rust', 'r1', 'x')])]);
		expect(g[1].accounts.map((a) => a.id)).toEqual(['a', 'b']);
	});

	it('lists the drops of a campaign per account, each in queue order', () => {
		const g = byGame([
			queue('a', [item('Rust', 'r1', 'x'), item('Rust', 'r1', 'y')]),
			queue('b', [item('Rust', 'r1', 'x'), item('Rust', 'r1', 'y')])
		]);
		expect(g[0].campaigns[0].drops.map((d) => `${d.accountId}:${d.target.benefit_id}`)).toEqual(['a:x', 'a:y', 'b:x', 'b:y']);
	});

	it('keeps campaigns of the same id on different platforms apart', () => {
		const g = byGame([
			queue('a', [item('Rust', 'c', 'x')]),
			queue('k', [item('Rust', 'c', 'y', 60, 0, 'kick')], 'kick')
		]);
		expect(g).toHaveLength(1);
		expect(g[0].campaigns.map((c) => c.platform)).toEqual(['twitch', 'kick']);
	});

	it('sums the minutes left, never below zero', () => {
		const g = byGame([queue('a', [item('Rust', 'r1', 'x', 60, 20), item('Rust', 'r1', 'y', 30, 45)])]);
		expect(g[0].minutesLeft).toBe(40);
	});

	it('is empty with no queues', () => {
		expect(byGame([])).toEqual([]);
	});
});

const gItem = (game: string, benefit: string): Target => ({
	game,
	platform: 'twitch',
	campaign_id: `${game}-c`,
	campaign_name: `${game} campaign`,
	benefit_id: benefit,
	benefit_name: benefit,
	image_url: '',
	required_minutes: 60,
	minutes: 0,
	ends_at: null,
	channels: [],
	channel_count: 0
});
const gRunner = (id: string, items: Target[]): Runner => ({ ...fx()[0], account_id: id, account_name: id, up_next: items, order_custom: false });

describe('reorderByGame', () => {
	const ids = (r: Runner) => r.up_next.map((x) => x.benefit_id);

	it('moves a game up in every runner that has it', () => {
		const rs = [gRunner('a', [gItem('Rust', 'r1'), gItem('Apex', 'p1')]), gRunner('b', [gItem('Rust', 'r2'), gItem('Apex', 'p2')])];
		const out = reorderByGame(rs, ['Apex', 'Rust']);
		expect(out.map(ids)).toEqual([
			['p1', 'r1'],
			['p2', 'r2']
		]);
		expect(out.every((r) => r.order_custom)).toBe(true);
	});

	it('sorts every runner to the full order, not only those with the moved game', () => {
		// Apex moved to the top; runner b has no Apex but its SMITE now ranks after Rust.
		const rs = [gRunner('a', [gItem('Rust', 'r1'), gItem('Apex', 'p1')]), gRunner('b', [gItem('SMITE', 's1'), gItem('Rust', 'r2')])];
		const out = reorderByGame(rs, ['Apex', 'Rust', 'SMITE']);
		expect(out.map(ids)).toEqual([
			['p1', 'r1'],
			['r2', 's1']
		]);
	});

	it('leaves a runner already in that order untouched', () => {
		const other = gRunner('b', [gItem('Rust', 'r2'), gItem('SMITE', 's1')]);
		const out = reorderByGame([gRunner('a', [gItem('Rust', 'r1'), gItem('Apex', 'p1')]), other], ['Apex', 'Rust', 'SMITE']);
		expect(out[1]).toBe(other);
		expect(ids(out[0])).toEqual(['p1', 'r1']);
	});

	it('keeps the drops of one game in their order', () => {
		const rs = [gRunner('a', [gItem('Rust', 'r1'), gItem('Apex', 'p1'), gItem('Rust', 'r2'), gItem('Apex', 'p2')])];
		expect(ids(reorderByGame(rs, ['Apex', 'Rust'])[0])).toEqual(['p1', 'p2', 'r1', 'r2']);
	});

	it('puts games missing from the order at the end in their relative place', () => {
		const rs = [gRunner('a', [gItem('New', 'n1'), gItem('Rust', 'r1'), gItem('Other', 'o1'), gItem('Apex', 'p1')])];
		expect(ids(reorderByGame(rs, ['Apex', 'Rust'])[0])).toEqual(['p1', 'r1', 'n1', 'o1']);
	});

	it('returns the same runner when its queue already follows the order', () => {
		const r = gRunner('a', [gItem('Apex', 'p1'), gItem('Rust', 'r1')]);
		expect(reorderByGame([r], ['Apex', 'Rust'])[0]).toBe(r);
	});
});

describe('commitGameOrder', () => {
	// Derived game order: Rust, SMITE, Apex.
	const start = () => [
		gRunner('a', [gItem('Rust', 'r1'), gItem('Apex', 'p1')]),
		gRunner('b', [gItem('SMITE', 's1')]),
		gRunner('c', [gItem('Rust', 'r2'), gItem('Rust', 'r3'), gItem('Apex', 'p2')])
	];
	const mk = (setOrder: () => Promise<void>, fetchRunners: () => Promise<Runner[]>) => {
		let cur = start();
		const calls: [string, string[]][] = [];
		const seen: Runner[][] = [];
		return {
			get cur() {
				return cur;
			},
			calls,
			seen,
			io: {
				get: () => cur,
				set: (r: Runner[]) => {
					seen.push(r);
					cur = r;
				},
				setOrder: async (id: string, ids: string[]) => {
					calls.push([id, ids]);
					return setOrder();
				},
				fetchRunners
			}
		};
	};

	it('moves a game up optimistically, saves changed runners only, then takes the server list', async () => {
		const server = start();
		const h = mk(async () => {}, async () => server);
		expect(await commitGameOrder(h.io, ['Apex', 'Rust', 'SMITE'])).toBe(true);
		expect(h.calls).toEqual([
			['a', ['p1', 'r1']],
			['c', ['p2', 'r2', 'r3']]
		]);
		expect(h.seen[0].map((r) => r.up_next.map((x) => x.benefit_id))).toEqual([['p1', 'r1'], ['s1'], ['p2', 'r2', 'r3']]);
		expect(h.cur).toBe(server);
	});
	it('an order that changes no queue makes no request', async () => {
		const h = mk(async () => {}, async () => start());
		// SMITE is only on runner b, which has nothing else to pass.
		expect(await commitGameOrder(h.io, ['SMITE', 'Rust', 'Apex'])).toBe(true);
		expect(h.calls).toEqual([]);
		expect(h.seen).toEqual([]);
	});
	it('failed save takes the server list', async () => {
		const server = start();
		const h = mk(() => Promise.reject(new Error('x')), async () => server);
		expect(await commitGameOrder(h.io, ['Apex', 'Rust', 'SMITE'])).toBe(false);
		expect(h.cur).toBe(server);
	});
	it('failed save and failed refetch roll back', async () => {
		const h = mk(() => Promise.reject(new Error('x')), () => Promise.reject(new Error('y')));
		const before = h.cur;
		expect(await commitGameOrder(h.io, ['Apex', 'Rust', 'SMITE'])).toBe(false);
		expect(h.cur).toBe(before);
	});
});

describe('inGameOrder', () => {
	const g = (game: string): GameGroup => ({ game, campaigns: [], accounts: [], drops: 1, minutesLeft: 0 });
	const names = (gs: GameGroup[]) => gs.map((x) => x.game);

	it('shows the groups in the dropped order', () => {
		expect(names(inGameOrder([g('Rust'), g('SMITE'), g('Apex')], ['Apex', 'Rust', 'SMITE']))).toEqual(['Apex', 'Rust', 'SMITE']);
	});
	it('puts a new game after, in its derived order, and ignores a vanished one', () => {
		const out = inGameOrder([g('New'), g('Rust'), g('Other'), g('Apex')], ['Apex', 'Gone', 'Rust']);
		expect(names(out)).toEqual(['Apex', 'Rust', 'New', 'Other']);
	});
	it('without an order keeps the derived order', () => {
		expect(names(inGameOrder([g('Rust'), g('Apex')], null))).toEqual(['Rust', 'Apex']);
	});
	it('a move that changes no queue still shows the dropped order', async () => {
		// Runner b holds SMITE alone, so moving it saves nothing; the display order still applies.
		const rs = [gRunner('a', [gItem('Rust', 'r1'), gItem('Apex', 'p1')]), gRunner('b', [gItem('SMITE', 's1')])];
		const derived = byGame(queues(rs));
		expect(names(derived)).toEqual(['Rust', 'SMITE', 'Apex']);
		const dropped = moveItem(names(derived), 1, 0);
		const calls: string[] = [];
		await commitGameOrder(
			{ get: () => rs, set: () => {}, setOrder: async (id) => void calls.push(id), fetchRunners: async () => rs },
			dropped
		);
		expect(calls).toEqual([]);
		const o = settleGameOrder({ games: dropped, custom: false }, rs);
		expect(names(inGameOrder(byGame(queues(rs)), o?.games ?? null))).toEqual(['SMITE', 'Rust', 'Apex']);
	});
});

describe('settleGameOrder', () => {
	const custom = (on: boolean) => [{ ...gRunner('a', [gItem('Rust', 'r1')]), order_custom: on }];

	it('clears once no runner is custom any more', () => {
		expect(settleGameOrder({ games: ['Rust'], custom: true }, custom(false))).toBeNull();
	});
	it('marks the order custom once a runner is', () => {
		expect(settleGameOrder({ games: ['Rust'], custom: false }, custom(true))).toEqual({ games: ['Rust'], custom: true });
	});
	it('keeps an order that never had a custom runner', () => {
		const o = { games: ['Rust'], custom: false };
		expect(settleGameOrder(o, custom(false))).toBe(o);
	});
	it('keeps the order while runners are not loaded', () => {
		const o = { games: ['Rust'], custom: true };
		expect(settleGameOrder(o, [])).toBe(o);
	});
});

describe('readGameOrder / writeGameOrder', () => {
	const mem = () => {
		const m = new Map<string, string>();
		return {
			getItem: (k: string) => m.get(k) ?? null,
			setItem: (k: string, v: string) => void m.set(k, v),
			removeItem: (k: string) => void m.delete(k),
			m
		};
	};
	it('round-trips and clears', () => {
		const s = mem();
		writeGameOrder({ games: ['Apex', 'Rust'], custom: true }, s);
		expect(readGameOrder(s)).toEqual({ games: ['Apex', 'Rust'], custom: true });
		writeGameOrder(null, s);
		expect(s.m.has(GAME_ORDER_KEY)).toBe(false);
		expect(readGameOrder(s)).toBeNull();
	});
	it('reads junk as no order', () => {
		const s = mem();
		s.setItem(GAME_ORDER_KEY, '{nope');
		expect(readGameOrder(s)).toBeNull();
		s.setItem(GAME_ORDER_KEY, JSON.stringify({ games: [1] }));
		expect(readGameOrder(s)).toBeNull();
	});
	it('survives storage that throws', () => {
		const bad = {
			getItem: () => {
				throw new Error('blocked');
			},
			setItem: () => {
				throw new Error('blocked');
			},
			removeItem: () => {
				throw new Error('blocked');
			}
		};
		expect(readGameOrder(bad)).toBeNull();
		expect(() => writeGameOrder({ games: [], custom: false }, bad)).not.toThrow();
	});
});

describe('withoutDrop', () => {
	it('removes the drop from that account only', () => {
		const a = gRunner('a', [gItem('Rust', 'r1'), gItem('Rust', 'r2')]);
		const b = gRunner('b', [gItem('Rust', 'r1')]);
		const out = withoutDrop([a, b], 'a', 'r1');
		expect(out[0].up_next.map((x) => x.benefit_id)).toEqual(['r2']);
		expect(out[1]).toBe(b);
	});
	it('leaves a runner without the drop as the same object', () => {
		const a = gRunner('a', [gItem('Rust', 'r1')]);
		expect(withoutDrop([a], 'a', 'zz')[0]).toBe(a);
	});
});
