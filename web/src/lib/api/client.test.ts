import { describe, expect, it, vi } from 'vitest';
import { ApiError, Unreachable, attachmentName, createClient } from './client';

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
		await api.setAccountOrder(['a1']);
		await api.removeAccount('a1');
		expect(hdr(calls[0], 'X-CSRF-Token')).toBeUndefined();
		for (const c of calls.slice(1)) expect(hdr(c, 'X-CSRF-Token')).toBe('tok');
	});

	it('recheckLinks posts with CSRF and returns counts', async () => {
		const { api, calls } = setup([{ status: 200, body: '{"claimed":1,"still_needs_link":0}' }]);
		expect(await api.recheckLinks('acc 1')).toEqual({ claimed: 1, still_needs_link: 0 });
		expect(calls[0].url).toBe('/api/accounts/acc%201/recheck-links');
		expect(calls[0].init.method).toBe('POST');
		expect(hdr(calls[0], 'X-CSRF-Token')).toBe('tok');
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

	it('campaign encodes the path', async () => {
		const { api, calls } = setup([{ status: 200, body: '{}' }]);
		await api.campaign('kick', 'a/b c');
		expect(calls[0].url).toBe('/api/campaigns/kick/a%2Fb%20c');
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

	it('changePassword posts current and new with CSRF', async () => {
		const { api, calls } = setup([{ status: 204 }]);
		await api.changePassword('old', 'newpass12');
		expect(calls[0].url).toBe('/api/settings/password');
		expect(hdr(calls[0], 'X-CSRF-Token')).toBe('tok');
		expect(JSON.parse(calls[0].init.body as string)).toEqual({ current: 'old', new: 'newpass12' });
	});

	it('changePassword 400 keeps the error code', async () => {
		const { api } = setup([{ status: 400, body: '{"error":"wrong_password"}' }]);
		await expect(api.changePassword('x', 'y')).rejects.toMatchObject({ status: 400, code: 'wrong_password' });
	});

	it('snapshot returns the file and its name', async () => {
		const f = vi.fn(async () => new Response('SQLite format 3', { status: 200, headers: { 'Content-Disposition': 'attachment; filename="grubdrops-20261026-2130.db"' } }));
		const api = createClient({ fetch: f as unknown as typeof fetch, csrf: () => 'tok', refreshCsrf: async () => {}, onUnauthorized: () => {}, onUnreachable: () => {} });
		const r = await api.snapshot();
		expect(r.name).toBe('grubdrops-20261026-2130.db');
		expect(await r.blob.text()).toBe('SQLite format 3');
	});

	it('snapshot error is an ApiError', async () => {
		const { api } = setup([{ status: 503, body: '{"error":"unavailable"}' }]);
		await expect(api.snapshot()).rejects.toMatchObject({ status: 503 });
	});

	it('attachmentName reads the filename or falls back', () => {
		expect(attachmentName('attachment; filename="a.db"')).toBe('a.db');
		expect(attachmentName('attachment; filename=b.db')).toBe('b.db');
		expect(attachmentName(null)).toBe('grubdrops.db');
	});

	it('checkLogin and restartAccount post with CSRF to the account', async () => {
		const { api, calls } = setup([{ status: 200, body: '{"ok":true,"detail":"ok","checked_at":null}' }, { status: 204 }]);
		expect(await api.checkLogin('acc 1')).toEqual({ ok: true, detail: 'ok', checked_at: null });
		await api.restartAccount('acc 1');
		expect(calls.map((c) => [c.init.method, c.url])).toEqual([
			['POST', '/api/accounts/acc%201/check-login'],
			['POST', '/api/accounts/acc%201/restart']
		]);
		for (const c of calls) expect(hdr(c, 'X-CSRF-Token')).toBe('tok');
	});

	it('markCollected posts the pair and removeClaim deletes the escaped path', async () => {
		const claim = '{"account_id":"a 1","benefit_id":"b/1","source":"manual"}';
		const { api, calls } = setup([{ status: 201, body: claim }, { status: 204 }]);
		expect((await api.markCollected('a 1', 'b/1')).source).toBe('manual');
		await api.removeClaim('a 1', 'b/1');
		expect(calls.map((c) => [c.init.method, c.url])).toEqual([
			['POST', '/api/claims'],
			['DELETE', '/api/claims/a%201/b%2F1']
		]);
		expect(JSON.parse(calls[0].init.body as string)).toEqual({ account_id: 'a 1', benefit_id: 'b/1' });
		for (const c of calls) expect(hdr(c, 'X-CSRF-Token')).toBe('tok');
	});

	it('setAccountGames sends the list only when given one', async () => {
		const view = '{"own":true,"games":[{"id":"g1","name":"Rust"}]}';
		const { api, calls } = setup([{ status: 200, body: view }, { status: 200, body: view }, { status: 200, body: view }]);
		expect((await api.accountGames('a/1')).games[0].name).toBe('Rust');
		await api.setAccountGames('a/1', true, [{ id: '', name: 'Rust' }]);
		await api.setAccountGames('a/1', false, null);
		expect(calls.map((c) => [c.init.method ?? 'GET', c.url])).toEqual([
			['GET', '/api/accounts/a%2F1/games'],
			['PUT', '/api/accounts/a%2F1/games'],
			['PUT', '/api/accounts/a%2F1/games']
		]);
		expect(JSON.parse(calls[1].init.body as string)).toEqual({ own: true, games: [{ id: '', name: 'Rust' }] });
		expect(JSON.parse(calls[2].init.body as string)).toEqual({ own: false });
		expect(hdr(calls[1], 'X-CSRF-Token')).toBe('tok');
	});
	it('forceWatch gets the list and putForceWatch puts it with CSRF', async () => {
		const list = '{"channels":[{"platform":"twitch","channel":"keshaeuw","live":true,"accounts":["a1"],"watching":[]}]}';
		const { api, calls } = setup([{ status: 200, body: list }, { status: 200, body: list }]);
		expect((await api.forceWatch()).channels[0].channel).toBe('keshaeuw');
		const sent = [{ platform: 'twitch' as const, channel: 'keshaeuw', accounts: ['a1'] }];
		expect((await api.putForceWatch(sent)).channels).toHaveLength(1);
		expect(calls.map((c) => [c.init.method ?? 'GET', c.url])).toEqual([
			['GET', '/api/force-watch'],
			['PUT', '/api/force-watch']
		]);
		expect(hdr(calls[0], 'X-CSRF-Token')).toBeUndefined();
		expect(hdr(calls[1], 'X-CSRF-Token')).toBe('tok');
		expect(hdr(calls[1], 'Content-Type')).toBe('application/json');
		expect(JSON.parse(calls[1].init.body as string)).toEqual({ channels: sent });
	});
});
