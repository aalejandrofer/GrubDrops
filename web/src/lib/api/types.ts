/** Wire types. Field names match the JSON from internal/api exactly. Times are RFC 3339 UTC or null. */

export type Platform = 'twitch' | 'kick';

export interface Session {
	authed: boolean;
	setup_needed: boolean;
	oidc: { enabled: boolean; name: string };
	csrf_token: string;
	version: string;
}

export type OverviewState = 'no_accounts' | 'no_games' | 'mining' | 'nothing_live' | 'idle';

export interface Problem {
	kind: 'needs_login';
	account_id: string;
	account_name: string;
}

export interface Overview {
	status: {
		state: OverviewState;
		accounts_total: number;
		accounts_mining: number;
		accounts_paused: number;
		last_scan_at: string | null;
		next_scan_at: string | null;
	};
	problems: Problem[];
	kpis: {
		minutes_total: number;
		minutes_today: number;
		claimed_today: number;
		claimed_all_time: number;
		accounts_healthy: number;
	};
	version: { current: string; latest: string; update_available: boolean };
}

export type RunnerState =
	| 'idle'
	| 'picking'
	| 'starting'
	| 'watching'
	| 'claiming'
	| 'sleeping'
	| 'awaiting_connect'
	| 'needs_login'
	| 'no_games';

export interface Target {
	game: string;
	platform: Platform;
	campaign_id: string;
	campaign_name: string;
	benefit_id: string;
	benefit_name: string;
	image_url: string;
	required_minutes: number;
	minutes: number;
	ends_at: string | null;
}

export interface Blocked {
	target: Target;
	reason: 'not_linked' | 'precondition' | 'skipped';
}

export interface Runner {
	account_id: string;
	account_name: string;
	platform: Platform;
	avatar_url: string;
	state: RunnerState;
	current: Target | null;
	channel: string;
	viewer_count: number;
	minutes: number;
	started_at: string | null;
	last_scan_at: string | null;
	up_next: Target[];
	blocked: Blocked[];
	order_custom: boolean;
}

export type GameState = 'mining' | 'waiting' | 'no_campaign';

export interface Campaign {
	id: string;
	platform: Platform;
	name: string;
	drops: number;
	ends_at: string | null;
}

export interface Game {
	id: string;
	name: string;
	slug: string;
	priority: number;
	box_art_url: string;
	state: GameState;
	campaigns: Campaign[];
}

export interface GamesResp {
	games: Game[];
	last_scan_at: string | null;
	next_scan_at: string | null;
}

export interface SearchHit {
	id: string;
	name: string;
	chosen: boolean;
	priority: number | null;
}

export interface ClaimsQuery {
	game?: string;
	account?: string;
	q?: string;
	limit?: number;
	offset?: number;
}

export interface Claim {
	account_id: string;
	account_name: string;
	platform: Platform;
	game: string;
	campaign_name: string;
	benefit_id: string;
	benefit_name: string;
	image_url: string;
	code: string;
	source: string;
	claimed_at: string;
}

export interface ClaimsResp {
	total: number;
	items: Claim[];
	games: string[];
	accounts: { id: string; name: string }[];
}

export interface Account {
	id: string;
	platform: Platform;
	name: string;
	avatar_url: string;
	state: string;
	needs_login: boolean;
	/** false: paused, the runner idles. */
	enabled: boolean;
	auth: { ok: boolean; detail: string; checked_at: string | null } | null;
}

export interface Settings {
	language: 'en' | 'es' | 'zh-CN';
	time_format: '24h' | '12h';
	theme: 'dark' | 'light';
	/** Empty means the server zone. */
	timezone: string;
	/** The zone the server resolves; added by the settings task. */
	zone_effective?: string;
	discord: { configured: boolean; webhook_hint: string };
	notify: { claim: boolean; auth: boolean; errors: boolean; daily_summary: boolean };
	update_auto: boolean;
}

export interface SettingsPatch {
	language?: Settings['language'];
	time_format?: Settings['time_format'];
	theme?: Settings['theme'];
	timezone?: string;
	notify?: Partial<Settings['notify']>;
	update_auto?: boolean;
	discord_webhook?: string;
}

export type HealthEventKind = 'claim' | 'progress' | 'state' | 'discovery' | 'error' | 'auth' | 'info';

export interface HealthEvent {
	at: string | null;
	level: string;
	kind: HealthEventKind | string;
	account_id: string;
	account_name: string;
	msg: string;
	fields?: Record<string, string>;
}

export interface Canary {
	platform: Platform;
	ok: boolean;
	skipped: boolean;
	detail: string;
	checked_at: string | null;
}

export interface Health {
	miner: { started_at: string | null; version: string; commit: string };
	kick_sidecars: { running: string[] };
	last_scan_at: string | null;
	canary: Canary[];
	events: HealthEvent[];
	counts: { all: number; problems: number; claims: number };
}

export interface UpdateInfo {
	current: string;
	latest: string;
	update_available: boolean;
	checked_at: string | null;
	notes: string[];
	notes_url: string;
	command: string;
	auto: boolean;
}

export interface DeviceStart {
	verification_url: string;
	user_code: string;
	expires_at: string;
}

export interface DeviceStatus {
	status: 'pending' | 'done' | 'expired' | 'error';
}

export interface ApiErrorBody {
	error: string;
	message?: string;
	priority?: number;
}
