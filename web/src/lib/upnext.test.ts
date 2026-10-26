import { describe, expect, it } from 'vitest';
import type { Runner } from '$lib/api/types';
import mining from '$lib/fixtures/runners.mining.json';
import { afterLabel, anyCustom, commitMove, foldQueue, moveItem, queues } from './upnext';

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
