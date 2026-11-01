import type { Platform, PointsMode } from '$lib/api/types';

export type VariantId = 'a' | 'b' | 'c';
export type Variant = { id: VariantId; name: string; tradeoff: string; note: string };

export const variants: Variant[] = [
	{
		id: 'a',
		name: 'A Side drawer',
		tradeoff: 'Keeps the account list in view and every section on one scroll; the drawer covers the list on phones.',
		note: 'Click a row (or its chevron) to slide a drawer in from the right: header, Login, Mining, Channel points, Danger, stacked. Click another row to swap accounts without closing. Full-width sheet under 720px.'
	},
	{
		id: 'b',
		name: 'B Centered modal with tabs',
		tradeoff: 'Short, focused tabs that leave room to grow; one more click to reach each section, and the list is hidden behind it.',
		note: 'The row opens a modal: header on top, then Login, Mining, Points and Danger tabs. A red dot on the Login tab flags an expired session. Prev and next arrows step through accounts. Near full screen on phones.'
	},
	{
		id: 'c',
		name: 'C Inline expanding row',
		tradeoff: 'No overlay and no context switch, several accounts open at once; the list gets long and rows jump while you read.',
		note: 'The row expands in place, like an accordion. The row is the header; the sections sit in a two-column grid under it on desktop and stack on phones. Several rows can be open at once.'
	}
];

/** How the account signed in. */
export type LoginType = 'android' | 'tv' | 'tdm' | 'cookies';

export const LOGIN_LABEL: Record<LoginType, string> = {
	android: 'Android app login',
	tv: 'TV code',
	tdm: 'Imported from TwitchDropsMiner',
	cookies: 'cookies.txt'
};

export type Doing =
	| { kind: 'mining'; game: string; drop: string; channel: string; m: number; req: number }
	| { kind: 'force'; channel: string }
	| { kind: 'sleep'; until: string }
	| { kind: 'idle' };

export type Acct = {
	id: string;
	platform: Platform;
	/** Custom name, else the platform name. */
	name: string;
	platform_name: string;
	enabled: boolean;
	needs_login: boolean;
	login: LoginType;
	/** When the current session was made, as shown. */
	signedIn: string;
	/** Last login check. */
	check: { ago: string; ok: boolean; detail: string };
	own_games: boolean;
	own: string[];
	skip_sleep: boolean;
	doing: Doing;
	/** First Up next drop, what Mine now would switch to. */
	next: { game: string; drop: string; m: number; req: number } | null;
	/** Twitch only. Set on the Points page. */
	points?: { mode: PointsMode; total: number; channels: number; fetched: string };
};

export const accounts: Acct[] = [
	{
		id: 't1',
		platform: 'twitch',
		name: 'TTik3r',
		platform_name: 'ttik3r',
		enabled: true,
		needs_login: false,
		login: 'android',
		signedIn: 'Sep 28',
		check: { ago: '4 min ago', ok: true, detail: '' },
		own_games: true,
		own: ['Rust', 'Apex Legends', 'Marvel Rivals'],
		skip_sleep: false,
		doing: { kind: 'mining', game: 'Rust', drop: 'Garage Door Skin', channel: 'hjune', m: 42, req: 60 },
		next: { game: 'Apex Legends', drop: 'Charm: Grub', m: 0, req: 120 },
		points: { mode: 'drops_points', total: 21750, channels: 4, fetched: '23 min ago' }
	},
	{
		id: 't2',
		platform: 'twitch',
		name: 'GrubAlt',
		platform_name: 'grubalt_tv',
		enabled: false,
		needs_login: false,
		login: 'tdm',
		signedIn: 'Oct 2',
		check: { ago: '3 d ago', ok: true, detail: '' },
		own_games: false,
		own: [],
		skip_sleep: true,
		doing: { kind: 'idle' },
		next: { game: 'Rust', drop: 'Garage Door Skin', m: 18, req: 60 },
		points: { mode: 'off', total: 10815, channels: 3, fetched: '3 d ago' }
	},
	{
		id: 't3',
		platform: 'twitch',
		name: 'Phluses',
		platform_name: 'phluses',
		enabled: true,
		needs_login: true,
		login: 'tv',
		signedIn: 'Sep 24',
		check: { ago: 'yesterday 23:10', ok: false, detail: 'token invalid (401)' },
		own_games: false,
		own: [],
		skip_sleep: false,
		doing: { kind: 'idle' },
		next: null,
		points: { mode: 'points_only', total: 97275, channels: 5, fetched: 'yesterday 23:05' }
	},
	{
		id: 'k1',
		platform: 'kick',
		name: 'TTik3r',
		platform_name: 'ttik3r',
		enabled: true,
		needs_login: false,
		login: 'cookies',
		signedIn: 'Oct 19',
		check: { ago: '2 min ago', ok: true, detail: '' },
		own_games: false,
		own: [],
		skip_sleep: false,
		doing: { kind: 'force', channel: 'xqc' },
		next: null
	},
	{
		id: 'k2',
		platform: 'kick',
		name: 'Phluses',
		platform_name: 'phluses',
		enabled: true,
		needs_login: false,
		login: 'cookies',
		signedIn: 'Oct 27',
		check: { ago: '11 min ago', ok: true, detail: '' },
		own_games: true,
		own: ['World of Warcraft', 'Rust'],
		skip_sleep: false,
		doing: { kind: 'sleep', until: '07:45' },
		next: { game: 'World of Warcraft', drop: 'Grub Mount', m: 55, req: 240 }
	}
];

export const MODE_LABEL: Record<PointsMode, string> = {
	off: 'Off',
	drops_points: 'Drops + points',
	points_only: 'Points only'
};

export const MODE_SUB: Record<PointsMode, string> = {
	off: 'No channel points and no Force watch.',
	drops_points: 'Drops first; Force watch channels when no drop is live, bonus claimed while watching.',
	points_only: 'Never mines drops; Force watch channels only, in order.'
};

export type Status = 'signed_out' | 'paused' | 'mining' | 'force_watching' | 'sleep_window' | 'idle';

export const STATUS_LABEL: Record<Status, string> = {
	signed_out: 'Signed out',
	paused: 'Paused',
	mining: 'Mining',
	force_watching: 'Force watching',
	sleep_window: 'Sleep window',
	idle: 'Idle'
};

/** Signed out wins, then paused, like the real accountStatus. */
export function status(a: Acct): Status {
	if (a.needs_login) return 'signed_out';
	if (!a.enabled) return 'paused';
	if (a.doing.kind === 'mining') return 'mining';
	if (a.doing.kind === 'force') return 'force_watching';
	if (a.doing.kind === 'sleep') return a.skip_sleep ? 'idle' : 'sleep_window';
	return 'idle';
}

export function line(a: Acct): string {
	const s = status(a);
	if (s === 'signed_out') return `Session expired ${a.check.ago}. Mining stopped.`;
	if (s === 'paused') return 'Paused. Not mining.';
	const d = a.doing;
	if (d.kind === 'mining') return `${d.game} · ${d.drop} on ${d.channel} · ${d.m} / ${d.req} min`;
	if (d.kind === 'force') return `Force watching ${d.channel}`;
	if (d.kind === 'sleep') return a.skip_sleep ? 'Looking for a drop' : `Sleep window until ${d.until}`;
	return 'No drop to mine right now';
}

export function initials(name: string): string {
	const parts = name.split(/[\s_\-.]+/).filter(Boolean);
	const s = parts.length > 1 ? parts[0][0] + parts[1][0] : name.slice(0, 2);
	return s.toUpperCase();
}

export function pts(n: number): string {
	return n.toLocaleString('en-US');
}
