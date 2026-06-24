import { afterEach, describe, expect, test, vi } from 'vitest';
import { pollingResource } from './poll.svelte';

afterEach(() => {
  vi.restoreAllMocks();
  // reset any document.hidden override
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => false });
});

describe('pollingResource', () => {
  test('fetches immediately and exposes the value', async () => {
    const fn = vi.fn(async () => 'A');
    const r = pollingResource(fn, 10_000);
    await vi.waitFor(() => expect(r.current).toBe('A'));
    expect(fn).toHaveBeenCalledTimes(1);
    r.stop();
  });

  test('refetches after the interval', async () => {
    let n = 0;
    const fn = vi.fn(async () => `v${++n}`);
    const r = pollingResource(fn, 100);
    await vi.waitFor(() => expect(r.current).toBe('v1'));
    await vi.waitFor(() => expect(r.current).toBe('v2'), { timeout: 1000 });
    r.stop();
  });

  test('keeps last value when a fetch rejects', async () => {
    let call = 0;
    const fn = vi.fn(async () => {
      call++;
      if (call === 1) return 'good';
      throw new Error('boom');
    });
    const r = pollingResource(fn, 20);
    await vi.waitFor(() => expect(r.current).toBe('good'));
    await vi.waitFor(() => expect(r.error?.message).toBe('boom'), { timeout: 1000 });
    expect(r.current).toBe('good'); // unchanged
    r.stop();
  });

  test('stop() halts further fetches', async () => {
    const fn = vi.fn(async () => 'x');
    const r = pollingResource(fn, 20);
    await vi.waitFor(() => expect(fn).toHaveBeenCalledTimes(1));
    r.stop();
    const after = fn.mock.calls.length;
    await new Promise((res) => setTimeout(res, 80));
    expect(fn.mock.calls.length).toBe(after);
  });

  test('pauses while the tab is hidden', async () => {
    const fn = vi.fn(async () => 'x');
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => true });
    const r = pollingResource(fn, 20);
    await new Promise((res) => setTimeout(res, 80));
    expect(fn).not.toHaveBeenCalled(); // hidden → no fetch
    r.stop();
  });
});
