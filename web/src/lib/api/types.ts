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
	/** null when no force watch channel is set. Older fixtures leave it out. */
	force_watch?: OverviewForceWatch;
}

/** Cached Twitch channel-points balance; null values mean no successful read yet. */
export interface ChannelPointsBalance {
	account_id: string;
	balance: number | null;
	updated_at: string | null;
}

/** One force watch channel. live null = not checked yet. accounts and watching hold account IDs. */
export interface ForceChannel {
	platform: Platform;
	channel: string;
	live: boolean | null;
	accounts: string[];
	watching: string[];
	points?: ChannelPointsBalance[];
}


/** PUT /api/force-watch body item. The server rejects any other field. */
export interface ForceChannelPut {
	platform: Platform;
	channel: string;
	accounts: string[];
}

/** Home tile: live channels, watched first, at most two. watchers holds account names. */
export type OverviewForceWatch = {
	live: { platform: Platform; channel: string; watchers: string[]; points?: ChannelPointsBalance[] }[];
	more: number;
	total: number;
} | null;


export type RunnerState =
	| 'idle'
	| 'picking'
	| 'starting'
	| 'watching'
	| 'claiming'
	| 'sleeping'
	| 'awaiting_connect'
	| 'needs_login'
	| 'no_games'
	| 'force_watching';

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
	/** Up to three of the channels the campaign is limited to; empty means any channel. */
	channels: string[];
	channel_count: number;
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

export type GameState = 'mining' | 'waiting' | 'done' | 'no_campaign';

export interface Campaign {
	id: string;
	platform: Platform;
	name: string;
	drops: number;
	ends_at: string | null;
}

/** An account that must link an external account before the game's drops can be claimed. */
export interface NeedsLink {
	account_id: string;
	account_name: string;
	platform: Platform;
	/** http(s) link page, '' when unknown. */
	url: string;
}

export interface Game {
	id: string;
	name: string;
	slug: string;
	/** Twitch's name for the game, '' until known. Twitch campaigns of a renamed game use it. */
	twitch_name: string;
	priority: number;
	box_art_url: string;
	state: GameState;
	campaigns: Campaign[];
	needs_link: NeedsLink[];
}

/** POST /api/accounts/{id}/recheck-links result. */
export interface LinkRecheck {
	claimed: number;
	still_needs_link: number;
}

export interface GamesResp {
	games: Game[];
	last_scan_at: string | null;
	next_scan_at: string | null;
}

export interface DiscoverReward {
	name: string;
	image_url: string;
	minutes: number;
}

export interface DiscoverCampaign {
	id: string;
	platform: Platform;
	game: string;
	box_art_url: string;
	name: string;
	starts_at: string | null;
	ends_at: string | null;
	link_url: string;
	needs_link: boolean;
	rewards: DiscoverReward[];
}

/** error: '' on success, else needs_android, timeout or failed. */
export interface DiscoverPlatform {
	ok: boolean;
	error: '' | 'needs_android' | 'timeout' | 'failed';
	count: number;
}

export interface DiscoverResp {
	fetched_at: string | null;
	next_refresh_at: string | null;
	platforms: Partial<Record<Platform, DiscoverPlatform>>;
	campaigns: DiscoverCampaign[];
}

export interface SearchHit {
	id: string;
	name: string;
	box_art_url: string;
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
	/** '' when the claim log has no campaign for it. */
	campaign_id?: string;
	benefit_id: string;
	benefit_name: string;
	image_url: string;
	code: string;
	source: string;
	claimed_at: string;
}

export type DropCellState = 'claimed' | 'claimable' | 'accruing' | 'eligible' | 'blocked';

/** One account's state on one drop of a campaign. */
export interface DropCell {
	account_id: string;
	account_name: string;
	platform: Platform;
	state: DropCellState;
	reason: string;
	minutes: number;
	claimed_at: string | null;
	/** The drop is in the account's runner plan, so it can be marked collected. */
	queued?: boolean;
}

export interface CampaignDrop {
	id: string;
	name: string;
	image_url: string;
	required_minutes: number;
	accounts: DropCell[];
}

/** live false: rebuilt from the claim log because no account scans it now. */
export interface CampaignDetail {
	id: string;
	platform: Platform;
	game: string;
	name: string;
	box_art_url: string;
	starts_at: string | null;
	ends_at: string | null;
	live: boolean;
	link_url: string;
	drops: CampaignDrop[];
}

export interface ClaimsResp {
	total: number;
	items: Claim[];
	games: string[];
	accounts: { id: string; name: string }[];
}

/** Last login check for an account. */
export interface AccountAuth {
	ok: boolean;
	detail: string;
	checked_at: string | null;
}

export interface Account {
	id: string;
	platform: Platform;
	/** Custom name, else the platform name. */
	name: string;
	/** The platform login. */
	platform_name: string;
	avatar_url: string;
	state: string;
	needs_login: boolean;
	/** false: paused, the runner idles. */
	enabled: boolean;
	/** true: mines its own game list, not the global one. */
	own_games: boolean;
	/** Games on the own list, 0 while own_games is off. */
	own_game_count: number;
	points_enabled: boolean;
	points_only: boolean;
	points_streams: number;

	auth: AccountAuth | null;
}

/** One game in an account's own list. A game without an id is added by name. */
export interface AccountGame {
	id: string;
	name: string;
}

/** GET/PUT /api/accounts/{id}/games. games is kept while own is off. */
export interface AccountGames {
	own: boolean;
	games: AccountGame[];
}
export interface PointsSafety {
	sleep_enabled: boolean;
	sleep_start: string;
	sleep_end: string;
	follow_raids: boolean;
}


export interface Settings {
	language: 'en' | 'es' | 'zh-CN' | 'tr';
	time_format: '24h' | '12h';
	theme: 'dark' | 'light';
	/** Empty means the server zone. */
	timezone: string;
	/** The zone the server resolves; added by the settings task. */
	zone_effective?: string;
	discord: { configured: boolean; webhook_hint: string };
	/** token_hint is empty when the token comes from the env default. */
	telegram: { configured: boolean; token_hint: string; chat_id: string };
	notify: { claim: boolean; auth: boolean; errors: boolean; daily_summary: boolean; progress: boolean; canary: boolean };
	update_auto: boolean;
	/** ordered mines games in list order; ending_soonest mines the campaign that ends first. */
	priority_mode: 'ordered' | 'ending_soonest';
	/** Minutes between campaign scans; one of SCAN_INTERVALS. */
	scan_interval_min: number;
	/** Leave a drop when a higher-ranked campaign goes live. */
	preempt: boolean;
	/** url hides the password as ***. Applies after a restart. */
	proxy: { enabled: boolean; url: string };
	/** Applies after a restart. */
	kick_watch_mode: 'auto' | 'browser' | 'ws';
	notify_avatar_url: string;
	/** interval_sec 0 means off; the interval applies after a restart. */
	canary: { twitch_channel: string; kick_channel: string; interval_sec: number };
}

export interface SettingsPatch {
	language?: Settings['language'];
	time_format?: Settings['time_format'];
	theme?: Settings['theme'];
	timezone?: string;
	notify?: Partial<Settings['notify']>;
	update_auto?: boolean;
	priority_mode?: Settings['priority_mode'];
	scan_interval_min?: number;
	preempt?: boolean;
	proxy_enabled?: boolean;
	/** The GET hint means unchanged. */
	proxy_url?: string;
	kick_watch_mode?: Settings['kick_watch_mode'];
	notify_avatar_url?: string;
	canary?: Settings['canary'];
	discord_webhook?: string;
	telegram_bot_token?: string;
	telegram_chat_id?: string;
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

export interface AndroidLogin {
	id: string;
	login: string;
	created: boolean;
}

export interface DeviceStatus {
	status: 'pending' | 'done' | 'expired' | 'error';
}

export interface ApiErrorBody {
	error: string;
	message?: string;
	priority?: number;
}
