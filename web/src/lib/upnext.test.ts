import { describe, expect, it } from 'vitest';
import type { Runner } from '$lib/api/types';
import mining from '$lib/fixtures/runners.mining.json';
import { afterLabel, anyCustom, moveItem, queues } from './upnext';

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
