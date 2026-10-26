import { describe, expect, it, vi } from 'vitest';
import { Unreachable } from '$lib/api/client';
import { retryUnreachable } from './retry';

function harness(results: (Error | null)[]) {
	let n = 0;
	const fn = vi.fn(async () => {
		const r = results[Math.min(n++, results.length - 1)];
		if (r) throw r;
	});
	const timers: (() => void)[] = [];
	const states: [boolean, number][] = [];
	const r = retryUnreachable(fn, {
		schedule: (f) => {
			timers.push(f);
			return () => {};
		},
		tick: () => () => {},
		onState: (d, s) => states.push([d, s])
	});
	return { r, fn, timers, states };
}

const settle = () => new Promise((r) => setTimeout(r, 0));

describe('retryUnreachable', () => {
	it('session fails, then succeeds on retry: leaves offline', async () => {
		const { r, fn, timers, states } = harness([new Unreachable(), null]);
		await settle();
		expect(states).toEqual([[true, 5]]);
		timers[0]();
		expect(await r.done).toBe(true);
		expect(fn).toHaveBeenCalledTimes(2);
		expect(states.at(-1)).toEqual([false, 0]);
	});
	it('other errors reject', async () => {
		const { r } = harness([new Error('boom')]);
		await expect(r.done).rejects.toThrow('boom');
	});
	it('stop cancels and resolves false', async () => {
		const { r, fn, timers } = harness([new Unreachable()]);
		await settle();
		r.stop();
		expect(await r.done).toBe(false);
		timers[0]?.();
		await settle();
		expect(fn).toHaveBeenCalledTimes(1);
	});
});
