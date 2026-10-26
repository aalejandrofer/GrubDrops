import { describe, expect, it, vi } from 'vitest';
import { ApiError, Unreachable, createClient } from './client';

type Call = { url: string; init: RequestInit };
type Reply = { status: number; body?: string } | Error;

function setup(replies: Reply[] | ((n: number) => Reply)) {
	const calls: Call[] = [];
	const f = vi.fn(async (url: string | URL | Request, init?: RequestInit) => {
		calls.push({ url: String(url), init: init ?? {} });
		const r = typeof replies === 'function' ? replies(calls.length - 1) : (replies[calls.length - 1] ?? replies[replies.length - 1]);
		if (r instanceof Error) throw r;
		return new Response(r.status === 204 ? null : (r.body ?? ''), { status: r.status });
	});
	const hooks = {
		csrf: () => 'tok',
		refreshCsrf: vi.fn(async () => {}),
		onUnauthorized: vi.fn(),
		onUnreachable: vi.fn()
	};
	const api = createClient({ fetch: f as unknown as typeof fetch, ...hooks });
	return { api, calls, hooks };
}

const hdr = (c: Call, k: string) => (c.init.headers as Record<string, string>)[k];

describe('client', () => {
	it('sends X-CSRF-Token on unsafe methods only', async () => {
		const { api, calls } = setup([{ status: 200, body: '{"games":[],"last_scan_at":null}' }, { status: 201, body: '{}' }, { status: 204 }]);
		await api.games();
		await api.addGame('Rust');
		await api.setGames(['a']);
		await api.renameAccount('a1', 'x');
		await api.removeAccount('a1');
		expect(hdr(calls[0], 'X-CSRF-Token')).toBeUndefined();
		for (const c of calls.slice(1)) expect(hdr(c, 'X-CSRF-Token')).toBe('tok');
	});

	it('json body and content-type on writes', async () => {
		const { api, calls } = setup([{ status: 204 }]);
		await api.setOrder('a1', ['b1', 'b2']);
		expect(calls[0].init.method).toBe('PUT');
		expect(hdr(calls[0], 'Content-Type')).toBe('application/json');
		expect(JSON.parse(calls[0].init.body as string)).toEqual({ benefit_ids: ['b1', 'b2'] });
	});

	it('401 calls onUnauthorized once', async () => {
		const { api, hooks } = setup([{ status: 401, body: '{"error":"unauthorized"}' }]);
		const rs = await Promise.allSettled([api.overview(), api.runners(), api.games(), api.accounts(), api.update()]);
		for (const r of rs) {
			expect(r.status).toBe('rejected');
			const e = (r as PromiseRejectedResult).reason;
			expect(e).toBeInstanceOf(ApiError);
			expect(e.status).toBe(401);
		}
		expect(hooks.onUnauthorized).toHaveBeenCalledTimes(1);
	});

	it('login 401 does not redirect', async () => {
		const { api, hooks } = setup([{ status: 401, body: '{"error":"unauthorized"}' }]);
		await expect(api.login('bad')).rejects.toMatchObject({ status: 401 });
		expect(hooks.onUnauthorized).not.toHaveBeenCalled();
	});

	it('csrf 403 refreshes session and retries once', async () => {
		const { api, hooks, calls } = setup([{ status: 403, body: '{"error":"csrf"}' }, { status: 204 }]);
		await expect(api.logout()).resolves.toBeUndefined();
		expect(hooks.refreshCsrf).toHaveBeenCalledTimes(1);
		expect(calls).toHaveLength(2);
		const twice = setup([{ status: 403, body: '{"error":"csrf"}' }]);
		await expect(twice.api.logout()).rejects.toMatchObject({ status: 403, code: 'csrf' });
		expect(twice.calls).toHaveLength(2);
		expect(twice.hooks.refreshCsrf).toHaveBeenCalledTimes(1);
	});

	it('network failure marks unreachable', async () => {
		const { api, hooks } = setup([new TypeError('fetch failed')]);
		await expect(api.overview()).rejects.toBeInstanceOf(Unreachable);
		expect(hooks.onUnreachable).toHaveBeenCalledWith(true);
	});

	it('502 upstream JSON is an ApiError, not unreachable', async () => {
		const { api, hooks } = setup([{ status: 502, body: '{"error":"upstream"}' }]);
		await expect(api.notifyTest()).rejects.toMatchObject({ status: 502, code: 'upstream' });
		expect(hooks.onUnreachable).not.toHaveBeenCalledWith(true);
	});

	it('500 HTML is unreachable', async () => {
		const { api, hooks } = setup([{ status: 500, body: '<html>oops</html>' }]);
		await expect(api.overview()).rejects.toBeInstanceOf(Unreachable);
		expect(hooks.onUnreachable).toHaveBeenCalledWith(true);
	});

	it('success clears unreachable', async () => {
		const { api, hooks } = setup([{ status: 200, body: '[]' }]);
		await api.runners();
		expect(hooks.onUnreachable).toHaveBeenCalledWith(false);
	});

	it('409 keeps body.priority', async () => {
		const { api } = setup([{ status: 409, body: '{"error":"conflict","priority":4}' }]);
		const e = await api.addGame('Rust').catch((x) => x);
		expect(e).toBeInstanceOf(ApiError);
		expect(e.code).toBe('conflict');
		expect(e.body.priority).toBe(4);
	});

	it('kick 400 keeps message', async () => {
		const { api, calls } = setup([{ status: 400, body: '{"error":"bad_request","message":"line 3: bad cookie"}' }]);
		const e = await api.kickSession('a1', 'junk').catch((x) => x);
		expect(e.body.message).toBe('line 3: bad cookie');
		expect(JSON.parse(calls[0].init.body as string)).toEqual({ cookies_txt: 'junk', channels: [] });
	});

	it('claims builds query string', async () => {
		const { api, calls } = setup([{ status: 200, body: '{}' }]);
		await api.claims({ game: 'Rust', account: 'a1', limit: 10, offset: 10 });
		await api.claims({ game: '', limit: 10 });
		await api.claims({});
		await api.claims({ game: 'Rust', limit: 10, offset: 0 });
		expect(calls[0].url).toBe('/api/claims?game=Rust&account=a1&limit=10&offset=10');
		expect(calls[1].url).toBe('/api/claims?limit=10');
		expect(calls[2].url).toBe('/api/claims');
		expect(calls[3].url).toBe('/api/claims?game=Rust&limit=10&offset=0');
		await api.claims({ q: 'a b%', limit: 20 });
		expect(calls[4].url).toBe('/api/claims?q=a+b%25&limit=20');
	});

	it('session authed true re-arms the 401 callback', async () => {
		let n = 0;
		const { api, hooks } = setup(() => {
			n++;
			return n === 2
				? { status: 200, body: '{"authed":true,"setup_needed":false,"oidc":{"enabled":false,"name":""},"csrf_token":"t","version":"v"}' }
				: { status: 401, body: '{"error":"unauthorized"}' };
		});
		await api.overview().catch(() => {});
		await api.session();
		await api.overview().catch(() => {});
		expect(hooks.onUnauthorized).toHaveBeenCalledTimes(2);
	});

	it('2xx with a non-JSON body is Unreachable', async () => {
		const { api, hooks } = setup([{ status: 200, body: '<!doctype html><html></html>' }]);
		await expect(api.games()).rejects.toBeInstanceOf(Unreachable);
		expect(hooks.onUnreachable).toHaveBeenLastCalledWith(true);
	});
});
