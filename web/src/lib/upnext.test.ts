import { describe, expect, it } from 'vitest';
import type { Runner } from '$lib/api/types';
import mining from '$lib/fixtures/runners.mining.json';
import {
	afterLabel,
	anyCustom,
	applyMineNow,
	canMineNow,
	commitMineNow,
	commitMove,
	foldQueue,
	mineTarget,
	moveItem,
	queues
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
