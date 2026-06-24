import { afterEach, describe, expect, test, vi } from 'vitest';
import { apiFetch, ApiError, apiSend } from './api';

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

test('apiSend sends X-CSRF-Token from cookie and posts JSON', async () => {
  vi.stubGlobal('document', { cookie: 'csrftoken=tok42' } as Document);
  const fetchMock = vi.fn(async () =>
    new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } }),
  );
  vi.stubGlobal('fetch', fetchMock);

  await apiSend<{ ok: boolean }>('/api/accounts/x/toggle', 'POST');

  const [, init] = fetchMock.mock.calls[0];
  const headers = new Headers(init.headers);
  expect(headers.get('X-CSRF-Token')).toBe('tok42');
  expect(headers.get('Content-Type')).toContain('application/json');
  expect(init.method).toBe('POST');
});

test('apiSend throws ApiError on 403 csrf', async () => {
  vi.stubGlobal('document', { cookie: 'csrftoken=tok42' } as Document);
  vi.stubGlobal('fetch', vi.fn(async () =>
    new Response(JSON.stringify({ error: { code: 'csrf', message: 'bad' } }), { status: 403, headers: { 'Content-Type': 'application/json' } }),
  ));
  await expect(apiSend('/api/x', 'POST')).rejects.toMatchObject({ code: 'csrf', status: 403 });
});
