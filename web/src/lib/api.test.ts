import { afterEach, describe, expect, test, vi } from 'vitest';
import { apiFetch, ApiError } from './api';

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('apiFetch', () => {
  test('returns parsed body on 200', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(JSON.stringify({ ok: true }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );
    const out = await apiFetch<{ ok: boolean }>('/api/x');
    expect(out.ok).toBe(true);
  });

  test('throws ApiError carrying the envelope code on 500', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(JSON.stringify({ error: { code: 'internal', message: 'boom' } }), {
          status: 500,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );
    await expect(apiFetch('/api/x')).rejects.toBeInstanceOf(ApiError);
    await expect(apiFetch('/api/x')).rejects.toMatchObject({ code: 'internal', status: 500 });
  });

  test('redirects to /login on 401 and never resolves', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('', { status: 401 })));
    const assignSpy = vi.spyOn(window.location, 'assign').mockImplementation(() => {});

    let settled = false;
    void apiFetch('/api/x').then(
      () => { settled = true; },
      () => { settled = true; },
    );
    await new Promise((r) => setTimeout(r, 10));

    expect(assignSpy).toHaveBeenCalledWith('/login');
    expect(settled).toBe(false);
  });
});
