import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '$lib/api/client';
import { REFRESH_DONE_MS, REFRESH_WAIT_MS, RefreshState, type RefreshOpts } from './refresh.svelte';

beforeEach(() => {
	vi.useFakeTimers();
});
afterEach(() => {
	vi.useRealTimers();
});

/** A refresh over a signal the test moves by hand; check() stands in for the effect. */
function setup(post: RefreshOpts['post'] = async () => {}) {
	let sig = 0;
	const onland = vi.fn();
	const r = new RefreshState({ post, signal: () => sig, onland });
	return {
		r,
		onland,
		bump() {
			sig++;
			r.check();
		}
	};
}

describe('RefreshState', () => {
	it('is busy until fresh data lands, then done for 5s', async () => {
		const { r, onland, bump } = setup();
		await r.run();
		expect(r.busy).toBe(true);
		expect(r.done).toBe(false);
		r.check();
		expect(r.busy).toBe(true);
		bump();
		expect(r.busy).toBe(false);
		expect(r.done).toBe(true);
		expect(onland).toHaveBeenCalledTimes(1);
		await vi.advanceTimersByTimeAsync(REFRESH_DONE_MS - 1);
		expect(r.done).toBe(true);
		await vi.advanceTimersByTimeAsync(1);
		expect(r.done).toBe(false);
		expect(r.busy).toBe(false);
	});

	it('treats a 429 as fresh data: done and onland, never failed', async () => {
		const { r, onland } = setup(async () => {
			throw new ApiError(429, { error: 'cooldown', retry_after_sec: 42 });
		});
		await r.run();
		expect(r.busy).toBe(false);
		expect(r.done).toBe(true);
		expect(r.failed).toBe(false);
		expect(onland).toHaveBeenCalledTimes(1);
		await vi.advanceTimersByTimeAsync(REFRESH_DONE_MS);
		expect(r.done).toBe(false);
	});

	it('goes back to idle, not done, when nothing lands in time', async () => {
		const { r, onland } = setup();
		await r.run();
		await vi.advanceTimersByTimeAsync(REFRESH_WAIT_MS - 1);
		expect(r.busy).toBe(true);
		await vi.advanceTimersByTimeAsync(1);
		expect(r.busy).toBe(false);
		expect(r.done).toBe(false);
		expect(onland).not.toHaveBeenCalled();
	});

	it('fails on any other error', async () => {
		const { r, onland } = setup(async () => {
			throw new ApiError(503, { error: 'unavailable' });
		});
		await r.run();
		expect(r.busy).toBe(false);
		expect(r.done).toBe(false);
		expect(r.failed).toBe(true);
		expect(onland).not.toHaveBeenCalled();
	});

	it('lands data that arrives before the start answers', async () => {
		let answer!: () => void;
		const { r, onland, bump } = setup(() => new Promise<void>((res) => (answer = res)));
		const p = r.run();
		bump();
		answer();
		await p;
		expect(r.busy).toBe(false);
		expect(r.done).toBe(true);
		expect(onland).toHaveBeenCalledTimes(1);
		await vi.advanceTimersByTimeAsync(REFRESH_WAIT_MS);
		expect(onland).toHaveBeenCalledTimes(1);
	});

	it('lands a start that answers with the data itself, even the same cached content', async () => {
		// Discover: the POST answers with the catalog, cached while under a
		// minute old. Each answer is a new object, so it still lands.
		let data: { campaigns: string[] } | null = { campaigns: ['a'] };
		const onland = vi.fn();
		const r = new RefreshState({
			post: async () => {
				data = { campaigns: ['a'] };
			},
			signal: () => data,
			onland
		});
		await r.run();
		expect(r.busy).toBe(true);
		r.check();
		expect(r.busy).toBe(false);
		expect(r.done).toBe(true);
		expect(onland).toHaveBeenCalledTimes(1);
		await vi.advanceTimersByTimeAsync(REFRESH_WAIT_MS);
		expect(r.done).toBe(false);
		expect(r.busy).toBe(false);
		expect(onland).toHaveBeenCalledTimes(1);
	});

	it('ignores clicks while busy and restarts from done', async () => {
		const post = vi.fn(async () => {});
		const { r, bump } = setup(post);
		await r.run();
		await r.run();
		expect(post).toHaveBeenCalledTimes(1);
		bump();
		expect(r.done).toBe(true);
		await r.run();
		expect(post).toHaveBeenCalledTimes(2);
		expect(r.busy).toBe(true);
		expect(r.done).toBe(false);
	});

	it('dispose stops the timers', async () => {
		const { r, bump } = setup();
		await r.run();
		bump();
		r.dispose();
		await vi.advanceTimersByTimeAsync(REFRESH_DONE_MS);
		expect(r.done).toBe(true);
	});
});
