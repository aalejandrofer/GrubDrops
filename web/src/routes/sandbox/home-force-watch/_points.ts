import type { Platform } from '$lib/api/types';

/** Round 2 (2026-11-01): channel points on the Home Points watch tile. */
export type PVariant = { id: 'a' | 'b' | 'c'; name: string; tradeoff: string; note: string; backend: string };

export const pvariants: PVariant[] = [
	{
		id: 'a',
		name: 'A Points total as the value',
		tradeoff: 'Reads like the other tiles, one big number; only one live channel fits on the status line.',
		note: 'The tile value becomes the Twitch points total across every account and channel. The live status shrinks to one line under it: the top live channel and who watches it, plus a count of the rest. Kick only lists keep today\'s tile.',
		backend: 'Existing data: sum of cached points_balances. Overview needs a points total and read state added.'
	},
	{
		id: 'b',
		name: 'B Balance on each live row',
		tradeoff: 'Keeps both live rows and adds the per-channel number; busiest tile, and on phones it needs a full row.',
		note: 'Today\'s tile with the total next to the label and a balance at the end of each live row (all accounts on that channel added up). Kick rows show no balance.',
		backend: 'Existing data: points_balances per channel. Overview needs a balance per live row and a total.'
	},
	{
		id: 'c',
		name: 'C Separate Points collected tile',
		tradeoff: 'Collected points read like Drops claimed, all time plus today; five tiles get tight and need a new log.',
		note: 'Points watch stays as today. A fifth tile after it counts points collected all time, today below, the same shape as Drops claimed. Hidden when no Twitch account has points on.',
		backend: 'Needs NEW backend data: a points earned log (new table) of watch and bonus gains. Balances only hold the current value.'
	}
];

export type PScene = 'watching' | 'nobody' | 'offline' | 'pending' | 'failed' | 'kick';
export const pscenes: { id: PScene; label: string }[] = [
	{ id: 'watching', label: 'Live, accounts watching' },
	{ id: 'nobody', label: 'Live, nobody watching' },
	{ id: 'offline', label: 'Nothing live' },
	{ id: 'pending', label: 'Balances not read yet' },
	{ id: 'failed', label: 'Last read failed' },
	{ id: 'kick', label: 'Kick channels only' }
];

export type LiveRow = { platform: Platform; channel: string; watchers: string[]; balance: number | null };

export type PState = {
	live: LiveRow[];
	more: number;
	total: number;
	/** Twitch points across all accounts and channels, null when never read or no Twitch channel. */
	points: number | null;
	read: 'ok' | 'pending' | 'failed' | 'none';
	/** Last good read, for the failed state. */
	readAgo: string;
	/** Variant C only: needs a new points earned log. */
	collected: { all: number; today: number } | null;
	mining: number;
};

export function pstate(s: PScene): PState {
	const twitchRows: LiveRow[] = [
		{ platform: 'twitch', channel: 'keshaeuw', watchers: ['Phluses'], balance: 31_420 },
		{ platform: 'twitch', channel: 'thebausffs', watchers: [], balance: 8_905 }
	];
	const base: PState = {
		live: twitchRows,
		more: 2,
		total: 5,
		points: 48_210,
		read: 'ok',
		readAgo: '',
		collected: { all: 23_460, today: 1_850 },
		mining: 3
	};
	switch (s) {
		case 'watching':
			return base;
		case 'nobody':
			return { ...base, live: twitchRows.map((r) => ({ ...r, watchers: [] })), mining: 4 };
		case 'offline':
			return { ...base, live: [], more: 0, mining: 4, collected: { all: 23_460, today: 0 } };
		case 'pending':
			return {
				...base,
				live: twitchRows.map((r) => ({ ...r, balance: null })),
				points: null,
				read: 'pending',
				collected: { all: 0, today: 0 }
			};
		case 'failed':
			return { ...base, read: 'failed', readAgo: '2 h ago' };
		case 'kick':
			return {
				...base,
				live: [{ platform: 'kick', channel: 'trainwreckstv', watchers: ['GrubKick'], balance: null }],
				more: 0,
				total: 2,
				points: null,
				read: 'none',
				collected: null
			};
	}
}

export const pts = (n: number) => n.toLocaleString('en-US');
