import type { Platform, PointsMode } from '$lib/api/types';

export type VariantId = 'a' | 'b' | 'c' | 'd';
export type Variant = { id: VariantId; name: string; tradeoff: string; note: string };

export const variants: Variant[] = [
	{
		id: 'a',
		name: 'A Channel-first',
		tradeoff: 'Order and who-watches-what read at a glance; per-account totals sit in a thin strip, easy to miss.',
		note: 'Ranked channel rows. Each row carries account chips: tap a chip to toggle that account, its balance on that channel sits inside the chip. Account modes and totals are a compact strip above the list.'
	},
	{
		id: 'b',
		name: 'B Account-first',
		tradeoff: 'Each account reads like a wallet with its mode and total up top; channel order lives in a second section, away from the toggles.',
		note: 'One card per account: status, mode, total balance, and every channel in global order with a toggle and its balance. The ordered list editor (drag, add, remove, live, watching) is its own section below.'
	},
	{
		id: 'c',
		name: 'C Matrix',
		tradeoff: 'Densest and easiest to compare across accounts; needs width, so phones get per-channel cards instead of the grid.',
		note: 'Channels as rows, accounts as columns. Each cell is a toggle plus that balance, the mode picker sits in the column header, totals in the footer row. Under 720px the grid folds into one card per channel.'
	},
	{
		id: 'd',
		name: 'D Hybrid of A and B',
		tradeoff: 'Wallets up top and the ordered list with toggles on one screen; the wallets push the list down, most on phones.',
		note: 'A\'s ranked rows with B\'s account wallets on top: mode, big total, freshness and status per account. Each row carries one tile per account in the same order as the wallets, so tiles line up in columns. Focus a wallet to fade the other accounts and see that account\'s channels and balances in order.'
	}
];

export type Acct = {
	id: string;
	name: string;
	platform: Platform;
	enabled: boolean;
	/** Twitch only. Kick has no channel points mode. */
	mode?: PointsMode;
	/** What the account does right now, for the status line. */
	doing: { kind: 'mining'; game: string } | { kind: 'watching'; channel: string } | { kind: 'idle' } | { kind: 'paused' };
	/** Twitch only: the last balance sweep. */
	fetch?: { minsAgo: number; failed: boolean };
};

export type Chan = {
	id: string;
	platform: Platform;
	name: string;
	live: boolean;
	/** Accounts ticked for this channel. */
	accounts: string[];
	/** Accounts watching it this minute. */
	watching: string[];
};

export const accounts: Acct[] = [
	{
		id: 't1',
		name: 'TTik3r',
		platform: 'twitch',
		enabled: true,
		mode: 'drops_points',
		doing: { kind: 'mining', game: 'Rust' },
		fetch: { minsAgo: 140, failed: true }
	},
	{
		id: 't2',
		name: 'Phluses',
		platform: 'twitch',
		enabled: true,
		mode: 'points_only',
		doing: { kind: 'watching', channel: 'keshaeuw' },
		fetch: { minsAgo: 23, failed: false }
	},
	{
		id: 't3',
		name: 'GrubAlt',
		platform: 'twitch',
		enabled: false,
		mode: 'off',
		doing: { kind: 'paused' },
		fetch: { minsAgo: 4320, failed: false }
	},
	{ id: 'k1', name: 'TTik3r', platform: 'kick', enabled: true, doing: { kind: 'watching', channel: 'xqc' } },
	{ id: 'k2', name: 'Phluses', platform: 'kick', enabled: true, doing: { kind: 'mining', game: 'Rust' } }
];

export const channels: Chan[] = [
	{ id: 'c1', platform: 'twitch', name: 'keshaeuw', live: true, accounts: ['t1', 't2'], watching: ['t2'] },
	{ id: 'c2', platform: 'twitch', name: 'thebausffs', live: true, accounts: ['t2', 't3'], watching: [] },
	{ id: 'c3', platform: 'twitch', name: 'caedrel', live: false, accounts: ['t1', 't2'], watching: [] },
	{ id: 'c4', platform: 'twitch', name: 'zackrawrr', live: true, accounts: ['t1'], watching: [] },
	{ id: 'c5', platform: 'twitch', name: 'tarik', live: false, accounts: ['t2'], watching: [] },
	{ id: 'c6', platform: 'twitch', name: 'shroud', live: true, accounts: ['t1', 't2', 't3'], watching: [] },
	{ id: 'c7', platform: 'twitch', name: 'lirik', live: false, accounts: ['t3'], watching: [] },
	{ id: 'c8', platform: 'twitch', name: 'summit1g', live: false, accounts: [], watching: [] },
	{ id: 'k1c', platform: 'kick', name: 'xqc', live: true, accounts: ['k1', 'k2'], watching: ['k1'] },
	{ id: 'k2c', platform: 'kick', name: 'trainwreckstv', live: false, accounts: ['k1'], watching: [] },
	{ id: 'k3c', platform: 'kick', name: 'adinross', live: true, accounts: ['k2'], watching: [] }
];

/** Cached balance per channel per Twitch account. Missing = never watched there. */
export const balances: Record<string, Record<string, number>> = {
	c1: { t1: 12450, t2: 48210, t3: 310 },
	c2: { t2: 9875, t3: 2140 },
	c3: { t1: 3020, t2: 15600 },
	c4: { t1: 870 },
	c5: { t2: 22300 },
	c6: { t1: 5410, t2: 1290, t3: 640 },
	c7: { t3: 7725 },
	c8: {}
};

export const MODES: { mode: PointsMode; label: string; short: string }[] = [
	{ mode: 'off', label: 'Off', short: 'Off' },
	{ mode: 'drops_points', label: 'Drops + points', short: 'Drops + pts' },
	{ mode: 'points_only', label: 'Points only', short: 'Pts only' }
];

export const MODE_SUB: Record<PointsMode, string> = {
	off: 'No points, skips Force watch.',
	drops_points: 'Drops first; Force watch when no drop is live.',
	points_only: 'Never mines drops; always on Force watch.'
};

/** Balance sweeps older than this read as stale. */
export const STALE_MINS = 120;

export function pts(n: number): string {
	return n.toLocaleString('en-US');
}

export function ago(mins: number): string {
	if (mins < 60) return `${mins} min ago`;
	if (mins < 60 * 24) return `${Math.round(mins / 60)} h ago`;
	return `${Math.round(mins / 60 / 24)} d ago`;
}

export type Fresh = 'fresh' | 'stale' | 'failed';

export function freshness(a: Acct): Fresh {
	if (!a.fetch) return 'fresh';
	if (a.fetch.failed) return 'failed';
	return a.fetch.minsAgo > STALE_MINS ? 'stale' : 'fresh';
}

export function freshText(a: Acct): string {
	if (!a.fetch) return '';
	if (a.fetch.failed) return `Couldn't fetch · showing ${ago(a.fetch.minsAgo)}`;
	return `Updated ${ago(a.fetch.minsAgo)}`;
}

export function statusText(a: Acct): string {
	switch (a.doing.kind) {
		case 'mining':
			return `Mining ${a.doing.game} drops`;
		case 'watching':
			return `Watching ${a.doing.channel}`;
		case 'paused':
			return 'Paused';
		default:
			return 'Idle';
	}
}

/** Why Force watch is or isn't running for this account right now. */
export function fwNote(a: Acct): string {
	if (!a.enabled) return 'Paused, skips Force watch';
	if (a.mode === 'off') return 'Points off, skips Force watch';
	if (a.mode === 'points_only') return 'Always on Force watch';
	if (a.doing.kind === 'mining') return 'Force watch waits for no drop';
	return 'On Force watch';
}

/** Mirrors the real parser: twitch.tv/<name> or kick.com/<name>. */
export function parseChannel(raw: string): { platform: Platform; name: string } | null {
	const s = raw.trim();
	if (!s) return null;
	let u: URL;
	try {
		u = new URL(/^https?:\/\//i.test(s) ? s : `https://${s}`);
	} catch {
		return null;
	}
	const host = u.hostname.replace(/^(www\.|m\.)/, '').toLowerCase();
	const name = u.pathname.split('/').filter(Boolean)[0]?.toLowerCase() ?? '';
	if (!/^[a-z0-9_]{2,25}$/.test(name)) return null;
	if (host === 'twitch.tv') return { platform: 'twitch', name };
	if (host === 'kick.com') return { platform: 'kick', name };
	return null;
}
