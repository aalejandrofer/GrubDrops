// Static data for the settings redesign mockups. Nothing is saved.

export type Hue = 'violet' | 'blue' | 'amber' | 'orange' | 'pink' | 'teal' | 'green' | 'red' | 'indigo';
export type IconName =
	| 'users'
	| 'sort'
	| 'moon'
	| 'globe'
	| 'flask'
	| 'send'
	| 'bell'
	| 'sliders'
	| 'pulse'
	| 'download'
	| 'lock'
	| 'cpu'
	| 'search'
	| 'chevron-right'
	| 'chevron-left'
	| 'chevron-down'
	| 'check'
	| 'x'
	| 'alert'
	| 'copy'
	| 'play'
	| 'refresh'
	| 'plus'
	| 'log-out'
	| 'grid'
	| 'chat';

export type SecId =
	| 'accounts'
	| 'priority'
	| 'sleep'
	| 'network'
	| 'experimental'
	| 'channels'
	| 'events'
	| 'general'
	| 'health'
	| 'updates'
	| 'security';

export type GroupId = 'accounts' | 'mining' | 'alerts' | 'system';

export type Sec = { id: SecId; label: string; icon: IconName; hue: Hue; blurb: string; group: GroupId };
export type Group = { id: GroupId; label: string; hue: Hue; icon: IconName; sections: SecId[] };

/** The redesigns split today's 8 sections into 11 smaller ones under 4 groups. */
export const SECS: Record<SecId, Sec> = {
	accounts: { id: 'accounts', label: 'Accounts', icon: 'users', hue: 'violet', blurb: 'Each account mines your games.', group: 'accounts' },
	priority: { id: 'priority', label: 'Drop priority', icon: 'sort', hue: 'orange', blurb: 'What gets mined first.', group: 'mining' },
	sleep: { id: 'sleep', label: 'Sleep window', icon: 'moon', hue: 'indigo', blurb: 'Pause mining every night.', group: 'mining' },
	network: { id: 'network', label: 'Network', icon: 'globe', hue: 'orange', blurb: 'Proxy and Kick watch mode.', group: 'mining' },
	experimental: { id: 'experimental', label: 'Experimental', icon: 'flask', hue: 'pink', blurb: 'May change or go away.', group: 'mining' },
	channels: { id: 'channels', label: 'Discord and Telegram', icon: 'send', hue: 'teal', blurb: 'Where alerts go.', group: 'alerts' },
	events: { id: 'events', label: 'Alerts', icon: 'bell', hue: 'teal', blurb: 'What sends a message.', group: 'alerts' },
	general: { id: 'general', label: 'General', icon: 'sliders', hue: 'blue', blurb: 'Language, time and theme.', group: 'system' },
	health: { id: 'health', label: 'Health', icon: 'pulse', hue: 'green', blurb: 'Miner activity and canary.', group: 'system' },
	updates: { id: 'updates', label: 'Updates', icon: 'download', hue: 'blue', blurb: 'No auto-update.', group: 'system' },
	security: { id: 'security', label: 'Authentication', icon: 'lock', hue: 'indigo', blurb: 'Single sign-on and admin password.', group: 'system' }
};

export const GROUPS: Group[] = [
	{ id: 'accounts', label: 'Accounts', hue: 'violet', icon: 'users', sections: ['accounts'] },
	{ id: 'mining', label: 'Mining', hue: 'orange', icon: 'cpu', sections: ['priority', 'sleep', 'network', 'experimental'] },
	{ id: 'alerts', label: 'Notifications', hue: 'teal', icon: 'bell', sections: ['channels', 'events'] },
	{ id: 'system', label: 'System', hue: 'blue', icon: 'sliders', sections: ['general', 'health', 'updates', 'security'] }
];

export const ORDER: SecId[] = GROUPS.flatMap((g) => g.sections);

/** Every searchable setting: row id is `set-<key>`. */
export type Entry = { key: string; label: string; sec: SecId; words?: string };
export const INDEX: Entry[] = [
	{ key: 'acc-list', label: 'Accounts', sec: 'accounts', words: 'twitch kick login ttik3r phluses' },
	{ key: 'acc-add', label: 'Add account', sec: 'accounts', words: 'android tv cookies' },
	{ key: 'pm', label: 'Priority mode', sec: 'priority', words: 'ending soonest order' },
	{ key: 'preempt', label: 'Switch to a higher-ranked drop', sec: 'priority' },
	{ key: 'raids', label: 'Follow raids', sec: 'priority', words: 'twitch' },
	{ key: 'scan', label: 'Check for new campaigns every', sec: 'priority', words: 'scan interval' },
	{ key: 'sleep-on', label: 'Pause mining every night', sec: 'sleep', words: 'sleep window night' },
	{ key: 'sleep-times', label: 'Sleep start and end', sec: 'sleep', words: 'hours time' },
	{ key: 'proxy', label: 'Use a proxy', sec: 'network', words: 'socks5 http' },
	{ key: 'kick-mode', label: 'Kick watch mode', sec: 'network', words: 'websocket browser sidecar' },
	{ key: 'share', label: 'Share Android campaigns with TV accounts', sec: 'experimental', words: 'catalog' },
	{ key: 'discord', label: 'Discord webhook', sec: 'channels' },
	{ key: 'telegram', label: 'Telegram bot', sec: 'channels', words: 'token chat id' },
	{ key: 'test', label: 'Test notification', sec: 'channels' },
	{ key: 'ev-claim', label: 'Drop claimed', sec: 'events' },
	{ key: 'ev-auth', label: 'Account needs login', sec: 'events' },
	{ key: 'ev-errors', label: 'Errors', sec: 'events' },
	{ key: 'ev-daily', label: 'Daily summary', sec: 'events' },
	{ key: 'ev-codes', label: 'Reward codes', sec: 'events' },
	{ key: 'ev-progress', label: 'Drop progress', sec: 'events' },
	{ key: 'ev-canary', label: 'Canary failures', sec: 'events' },
	{ key: 'avatar', label: 'Notification avatar URL', sec: 'events', words: 'discord image' },
	{ key: 'mention', label: 'Mention on errors', sec: 'events', words: 'user id ping' },
	{ key: 'lang', label: 'Language', sec: 'general' },
	{ key: 'timefmt', label: 'Time format', sec: 'general', words: '24h 12h' },
	{ key: 'theme', label: 'Theme', sec: 'general', words: 'dark light' },
	{ key: 'tz', label: 'Timezone', sec: 'general' },
	{ key: 'signout', label: 'Sign out', sec: 'general' },
	{ key: 'canary', label: 'Canary', sec: 'health', words: 'watch time credit run now' },
	{ key: 'log', label: 'Activity log', sec: 'health', words: 'events errors claims' },
	{ key: 'canary-cfg', label: 'Canary interval and channels', sec: 'health' },
	{ key: 'update-check', label: 'Check for updates', sec: 'updates', words: 'version' },
	{ key: 'update-auto', label: 'Check automatically', sec: 'updates' },
	{ key: 'sso', label: 'Single sign-on', sec: 'security', words: 'sso authentik oidc login' },
	{ key: 'pw-login', label: 'Password sign-in', sec: 'security', words: 'sso authentik oidc' },
	{ key: 'pw-change', label: 'Change password', sec: 'security' },
	{ key: 'snapshot', label: 'Database snapshot', sec: 'general', words: 'backup download' }
];

export type Status = 'mining' | 'points' | 'paused' | 'signed_out';
export type Acct = {
	id: string;
	name: string;
	platform: 'twitch' | 'kick';
	login: string;
	status: Status;
	line: string;
};

export const ACCOUNTS: Acct[] = [
	{ id: 'a1', name: 'TTik3r', platform: 'twitch', login: 'Android', status: 'mining', line: 'Mining: War Thunder' },
	{ id: 'a2', name: 'Phluses', platform: 'twitch', login: 'TV code', status: 'points', line: 'Points watching lirik' },
	{ id: 'a3', name: 'TTik3r', platform: 'kick', login: 'cookies.txt', status: 'mining', line: 'Mining: Rust' },
	{ id: 'a4', name: 'Phluses', platform: 'kick', login: 'cookies.txt', status: 'signed_out', line: 'Session expired 2 days ago' }
];

export const STATUS_LABEL: Record<Status, string> = {
	mining: 'Mining',
	points: 'Points watching',
	paused: 'Paused',
	signed_out: 'Signed out'
};

export type LogKind = 'claim' | 'progress' | 'state' | 'discovery' | 'error' | 'auth';
export const LOG: { at: string; kind: LogKind; who?: { name: string; platform: 'twitch' | 'kick' }; msg: string }[] = [
	{ at: '14:31', kind: 'claim', who: { name: 'TTik3r', platform: 'twitch' }, msg: 'Claimed Pz.Kpfw. IV decal on gaijinent' },
	{ at: '14:02', kind: 'progress', who: { name: 'TTik3r', platform: 'kick' }, msg: 'Watch progress: Rust Twitch Drops, 45 / 120 min' },
	{ at: '13:47', kind: 'error', who: { name: 'TTik3r', platform: 'kick' }, msg: 'Canary run failed: no watch time credit after 10 min' },
	{ at: '13:15', kind: 'discovery', msg: 'Picked a drop for War Thunder' },
	{ at: '12:58', kind: 'state', who: { name: 'Phluses', platform: 'twitch' }, msg: 'State changed: idle to Points watching' },
	{ at: '09:12', kind: 'auth', who: { name: 'Phluses', platform: 'kick' }, msg: 'Sign-in needed: session expired' }
];

export const LOG_KIND: Record<LogKind, { label: string; tone: string }> = {
	claim: { label: 'Claim', tone: 'ok' },
	progress: { label: 'Progress', tone: 'info' },
	state: { label: 'State', tone: 'off' },
	discovery: { label: 'Discovery', tone: 'violet' },
	error: { label: 'Error', tone: 'bad' },
	auth: { label: 'Auth', tone: 'warn' }
};

export const LANGS = ['English', 'Español', 'Türkçe', '简体中文'];
export const ZONES = ['Server default (Europe/Madrid)', 'Europe/London', 'America/New_York', 'Asia/Shanghai'];
export const SCANS = [5, 10, 15, 30, 60];
export const CANARY_EVERY = ['Off', '3 hours', '6 hours', '12 hours', '24 hours'];
