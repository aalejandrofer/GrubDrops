import type { Account, Platform, PointsMode } from '$lib/api/types';

/**
 * Points watch timer (2026-11-01): how long each account has been on its
 * current Points watch channel. The time is runner.started_at, which
 * beginForce (internal/runner/force.go) already sets when the runner enters
 * force_watching, so no new API field is needed.
 */
export type VariantId = 'a' | 'b' | 'c' | 'd';

export type Variant = {
	id: VariantId;
	name: string;
	tradeoff: string;
	note: string;
	/** Where the timer shows. */
	home: boolean;
	head: boolean;
	card: boolean;
	order: boolean;
};

export const variants: Variant[] = [
	{
		id: 'a',
		name: 'A On the channel rows',
		tradeoff: 'Time sits next to the channel it belongs to; the account cards and header stay as today.',
		note: 'Home tile rows read "shroud  seed_twitch 37m  41,870". The Points watch order row reads "Watching now · seed_twitch 37m". A channel with two watchers shows the longest time.',
		home: true,
		head: false,
		card: false,
		order: true
	},
	{
		id: 'b',
		name: 'B On the account, Points page only',
		tradeoff: 'One time per account, where the account lives; Home stays untouched, so the glance view has no time.',
		note: 'The account card line becomes "Watching shroud · 37m". The header status adds the time after each pair: "seed_twitch on shroud 37m".',
		home: false,
		head: true,
		card: true,
		order: false
	},
	{
		id: 'c',
		name: 'C Home tile and account card',
		tradeoff: 'The glance view and the per-account view both get it; the long header and the order row stay as they are.',
		note: 'Home tile rows get the time after the watcher, as in A. The Points account card line becomes "Watching shroud · 37m", as in B. Nothing else changes.',
		home: true,
		head: false,
		card: true,
		order: false
	},
	{
		id: 'd',
		name: 'D Account cards as a mini dashboard',
		tradeoff: 'Each card answers "what is this account doing and what has it earned"; six cards take more room above the order list.',
		note: 'Six Twitch accounts: three watching, one idle, one Drops only, one paused. Each card shows its state with the timer, its points total and the mode switch. With more than two watching, the header collapses to "Points watching · 3 accounts".',
		home: false,
		head: false,
		card: true,
		order: false
	}
];

const MIN = 60_000;

/** A fake account plus what its runner and balances would say. */
export type Acc = {
	a: Account;
	/** Channel it watches now, or null. */
	channel: string | null;
	/** Minutes on that channel when the page loaded. */
	onFor: number;
	/** Other state line when not watching. */
	line: string;
	points: number | null;
	/** Minutes since the balances were read. */
	readAgo: number;
	stale?: boolean;
};

function acct(id: string, name: string, platform: Platform, mode: PointsMode, enabled = true): Account {
	return {
		id,
		platform,
		name,
		platform_name: name,
		avatar_url: '',
		state: 'ok',
		needs_login: false,
		enabled,
		own_games: false,
		own_game_count: 0,
		skip_sleep: false,
		points_mode: mode,
		auth: null
	};
}

/** Variants A to C: today's dev seed, one Twitch and one Kick account. */
export const pair: Acc[] = [
	{
		a: acct('acc_tw', 'seed_twitch', 'twitch', 'drops_points'),
		channel: 'shroud',
		onFor: 37,
		line: '',
		points: 48_210,
		readAgo: 12
	},
	{
		a: acct('acc_kick', 'seed_kick', 'kick', 'drops_points'),
		channel: 'trainwreckstv',
		onFor: 72,
		line: '',
		points: null,
		readAgo: 0
	}
];

/** Variant D: six Twitch accounts. */
export const six: Acc[] = [
	{
		a: acct('acc_1', 'seed_twitch', 'twitch', 'drops_points'),
		channel: 'shroud',
		onFor: 37,
		line: '',
		points: 48_210,
		readAgo: 9
	},
	{
		a: acct('acc_2', 'grub_alt', 'twitch', 'points_only'),
		channel: 'xqc',
		onFor: 72,
		line: '',
		points: 12_905,
		readAgo: 9
	},
	{
		a: acct('acc_3', 'night_owl', 'twitch', 'points_only'),
		channel: 'summit1g',
		onFor: 248,
		line: '',
		points: 103_440,
		readAgo: 9
	},
	{
		a: acct('acc_4', 'spare_one', 'twitch', 'points_only'),
		channel: null,
		onFor: 0,
		line: 'Idle',
		points: 6_340,
		readAgo: 9
	},
	{
		a: acct('acc_5', 'drops_main', 'twitch', 'off'),
		channel: null,
		onFor: 0,
		line: 'Mining: Rust',
		points: 3_120,
		readAgo: 2 * 1440,
		stale: true
	},
	{
		a: acct('acc_6', 'old_main', 'twitch', 'drops_points', false),
		channel: null,
		onFor: 0,
		line: 'Paused',
		points: 21_775,
		readAgo: 3 * 1440,
		stale: true
	}
];

/** Watch order rows: channel, live, and who is ticked with a balance. */
export type Row = { channel: string; platform: Platform; live: boolean; ticks: { id: string; bal: number | null }[] };

export const pairOrder: Row[] = [
	{ channel: 'shroud', platform: 'twitch', live: true, ticks: [{ id: 'acc_tw', bal: 41_870 }] },
	{ channel: 'xqc', platform: 'twitch', live: false, ticks: [{ id: 'acc_tw', bal: 6_340 }] },
	{ channel: 'trainwreckstv', platform: 'kick', live: true, ticks: [{ id: 'acc_kick', bal: null }] }
];

export const sixOrder: Row[] = [
	{
		channel: 'shroud',
		platform: 'twitch',
		live: true,
		ticks: [
			{ id: 'acc_1', bal: 41_870 },
			{ id: 'acc_6', bal: 9_480 }
		]
	},
	{
		channel: 'xqc',
		platform: 'twitch',
		live: true,
		ticks: [
			{ id: 'acc_1', bal: 6_340 },
			{ id: 'acc_2', bal: 12_905 }
		]
	},
	{ channel: 'summit1g', platform: 'twitch', live: true, ticks: [{ id: 'acc_3', bal: 103_440 }] },
	{ channel: 'lirik', platform: 'twitch', live: false, ticks: [{ id: 'acc_4', bal: 4_190 }] }
];

/** started_at as the API sends it, for an account onFor minutes in at load. */
export function startedAt(loadedAt: number, onFor: number): string {
	// 20s past the minute, so the first turnover lands during the visit.
	return new Date(loadedAt - onFor * MIN - 20_000).toISOString();
}

export const pts = (n: number) => n.toLocaleString('en-US');
