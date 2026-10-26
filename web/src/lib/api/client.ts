import type {
	Account,
	AndroidLogin,
	ApiErrorBody,
	ClaimsQuery,
	ClaimsResp,
	DeviceStart,
	DeviceStatus,
	GamesResp,
	Game,
	Health,
	Overview,
	Platform,
	Runner,
	SearchHit,
	Session,
	Settings,
	SettingsPatch,
	UpdateInfo
} from './types';

export class ApiError extends Error {
	status: number;
	code: string;
	body: ApiErrorBody;
	constructor(status: number, body: ApiErrorBody) {
		super(body.message ?? body.error);
		this.name = 'ApiError';
		this.status = status;
		this.code = body.error;
		this.body = body;
	}
}

export class Unreachable extends Error {
	constructor(message = 'unreachable') {
		super(message);
		this.name = 'Unreachable';
	}
}

export interface ClientOpts {
	fetch?: typeof fetch;
	csrf: () => string;
	refreshCsrf: () => Promise<void>;
	onUnauthorized: () => void;
	onUnreachable: (down: boolean) => void;
}

export interface Api {
	session(): Promise<Session>;
	setup(password: string, confirm: string): Promise<void>;
	login(password: string): Promise<void>;
	logout(): Promise<void>;
	overview(): Promise<Overview>;
	runners(): Promise<Runner[]>;
	setOrder(accountId: string, benefitIds: string[]): Promise<void>;
	resetOrder(accountId: string): Promise<void>;
	mineNow(accountId: string): Promise<{ benefit_id: string }>;
	games(): Promise<GamesResp>;
	addGame(name: string): Promise<Game>;
	scanGames(): Promise<void>;
	setGames(ids: string[]): Promise<void>;
	searchGames(q: string): Promise<SearchHit[]>;
	claims(q: ClaimsQuery): Promise<ClaimsResp>;
	accounts(): Promise<Account[]>;
	addAccount(platform: Platform, name?: string): Promise<{ id: string }>;
	renameAccount(id: string, name: string): Promise<void>;
	removeAccount(id: string): Promise<void>;
	setAccountEnabled(id: string, enabled: boolean): Promise<void>;
	twitchStart(id: string): Promise<DeviceStart>;
	twitchStatus(id: string): Promise<DeviceStatus>;
	twitchAndroidUrl(): Promise<{ login_url: string }>;
	/** accountId signs that account in again; without it the server matches or creates the account. */
	twitchAndroidLogin(url: string, accountId?: string): Promise<AndroidLogin>;
	kickSession(id: string, cookiesTxt: string): Promise<{ verified: boolean }>;
	settings(): Promise<Settings>;
	saveSettings(p: SettingsPatch): Promise<Settings>;
	notifyTest(target?: 'discord' | 'telegram'): Promise<void>;
	health(filter: 'all' | 'problems' | 'claims'): Promise<Health>;
	runCanary(): Promise<void>;
	update(): Promise<UpdateInfo>;
	checkUpdate(): Promise<UpdateInfo>;
}

function parseBody(text: string): ApiErrorBody | null {
	try {
		const v: unknown = JSON.parse(text);
		if (v && typeof v === 'object' && typeof (v as ApiErrorBody).error === 'string') {
			return v as ApiErrorBody;
		}
	} catch {
		// not JSON
	}
	return null;
}

export function createClient(opts: ClientOpts): Api {
	const doFetch: typeof fetch = opts.fetch ?? ((...a) => globalThis.fetch(...a));
	let unauthorizedSent = false;

	function fireUnauthorized(): void {
		if (unauthorizedSent) return;
		unauthorizedSent = true;
		opts.onUnauthorized();
	}

	async function send(method: string, path: string, body?: unknown, redirect = true): Promise<unknown> {
		const unsafe = method !== 'GET' && method !== 'HEAD';
		for (let attempt = 0; ; attempt++) {
			const headers: Record<string, string> = {};
			if (unsafe) headers['X-CSRF-Token'] = opts.csrf();
			let payload: string | undefined;
			if (body !== undefined) {
				headers['Content-Type'] = 'application/json';
				payload = JSON.stringify(body);
			}
			let res: Response;
			try {
				res = await doFetch(path, { method, headers, body: payload });
			} catch {
				opts.onUnreachable(true);
				throw new Unreachable();
			}
			const text = res.status === 204 ? '' : await res.text();
			if (res.ok) {
				if (text === '') {
					opts.onUnreachable(false);
					return undefined;
				}
				let data: unknown;
				try {
					data = JSON.parse(text);
				} catch {
					// 2xx with a non-JSON body, e.g. the SPA fallback page from a proxy
					opts.onUnreachable(true);
					throw new Unreachable('non-JSON response');
				}
				opts.onUnreachable(false);
				return data;
			}
			const parsed = parseBody(text);
			if (parsed === null && res.status !== 401) {
				opts.onUnreachable(true);
				throw new Unreachable(`HTTP ${res.status}`);
			}
			opts.onUnreachable(false);
			const errBody = parsed ?? { error: 'unauthorized' };
			if (res.status === 403 && errBody.error === 'csrf' && attempt === 0) {
				await opts.refreshCsrf();
				continue;
			}
			if (res.status === 401 && redirect) fireUnauthorized();
			throw new ApiError(res.status, errBody);
		}
	}

	const get = <T>(path: string) => send('GET', path) as Promise<T>;
	const post = <T>(path: string, body?: unknown) => send('POST', path, body) as Promise<T>;
	const enc = encodeURIComponent;

	return {
		async session() {
			const s = (await get<Session>('/api/session'));
			if (s.authed) unauthorizedSent = false;
			return s;
		},
		setup: (password, confirm) => post('/api/setup', { password, confirm }),
		async login(password) {
			await send('POST', '/api/login', { password }, false);
			unauthorizedSent = false;
		},
		logout: () => post('/api/logout'),
		overview: () => get('/api/overview'),
		runners: () => get('/api/runners'),
		setOrder: (id, benefitIds) =>
			send('PUT', `/api/runners/${enc(id)}/order`, { benefit_ids: benefitIds }) as Promise<void>,
		resetOrder: (id) => send('DELETE', `/api/runners/${enc(id)}/order`) as Promise<void>,
		mineNow: (id) => post(`/api/runners/${enc(id)}/mine-now`),
		games: () => get('/api/games'),
		addGame: (name) => post('/api/games', { name }),
		scanGames: () => post('/api/games/scan'),
		setGames: (ids) => send('PUT', '/api/games', { ids }) as Promise<void>,
		searchGames: (q) => get(`/api/games/search?q=${enc(q)}`),
		claims(q) {
			const p = new URLSearchParams();
			if (q.game) p.set('game', q.game);
			if (q.account) p.set('account', q.account);
			if (q.q) p.set('q', q.q);
			if (q.limit !== undefined) p.set('limit', String(q.limit));
			if (q.offset !== undefined) p.set('offset', String(q.offset));
			const qs = p.toString();
			return get(`/api/claims${qs ? `?${qs}` : ''}`);
		},
		accounts: () => get('/api/accounts'),
		addAccount: (platform, name) => post('/api/accounts', name ? { platform, name } : { platform }),
		renameAccount: (id, name) => send('PATCH', `/api/accounts/${enc(id)}`, { name }) as Promise<void>,
		removeAccount: (id) => send('DELETE', `/api/accounts/${enc(id)}`) as Promise<void>,
		setAccountEnabled: (id, enabled) =>
			send('PUT', `/api/accounts/${enc(id)}/enabled`, { enabled }) as Promise<void>,
		twitchStart: (id) => post(`/api/accounts/${enc(id)}/twitch/device`),
		twitchStatus: (id) => get(`/api/accounts/${enc(id)}/twitch/device`),
		twitchAndroidUrl: () => get('/api/login/twitch/android'),
		twitchAndroidLogin: (url, accountId) =>
			post('/api/login/twitch/android', accountId ? { url, account_id: accountId } : { url }),
		kickSession: (id, cookiesTxt) =>
			post(`/api/accounts/${enc(id)}/kick/session`, { cookies_txt: cookiesTxt, channels: [] }),
		settings: () => get('/api/settings'),
		saveSettings: (p) => send('PUT', '/api/settings', p) as Promise<Settings>,
		notifyTest: (target) => post(`/api/settings/notify-test${target ? `?target=${target}` : ''}`),
		health: (filter) => get(`/api/health?filter=${filter}`),
		runCanary: () => post('/api/health/canary'),
		update: () => get('/api/update'),
		checkUpdate: () => post('/api/update/check')
	};
}
