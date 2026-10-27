import type { Platform } from '$lib/api/types';

export type Variant = { id: 'a' | 'b' | 'c'; name: string; note: string };

export const variants: Variant[] = [
	{
		id: 'a',
		name: 'A Fourth KPI tile',
		note: 'A Force watch tile in third place, before Accounts healthy. Shows the channel being watched and who watches it. Links to the panel on Games.'
	},
	{
		id: 'b',
		name: 'B Section above Up next',
		note: 'A small Force watch section at the top of the right column, one row per channel with its live dot and who is on it.'
	},
	{
		id: 'c',
		name: 'C Next to the mining status',
		note: 'A second status next to "Mining · 3 of 4 accounts" in the page header, for example "Force watching keshaeuw · 1 account".'
	}
];

export type Scene = 'watching' | 'standby' | 'offline' | 'empty';
export const scenes: { id: Scene; label: string }[] = [
	{ id: 'watching', label: 'One account force watching' },
	{ id: 'standby', label: 'Live, all accounts on drops' },
	{ id: 'offline', label: 'No forced channel live' },
	{ id: 'empty', label: 'No channels added (hidden)' }
];

export type Row = { platform: Platform; name: string; live: boolean; watchers: string[]; assigned: number };

/** Every forced channel in a scene. watchers = accounts on that channel now. */
export function all(s: Scene): Row[] {
	if (s === 'empty') return [];
	const live = s !== 'offline';
	return [
		{ platform: 'twitch', name: 'keshaeuw', live, watchers: s === 'watching' ? ['Phluses'] : [], assigned: 2 },
		{ platform: 'kick', name: 'xqc', live: false, watchers: [], assigned: 1 },
		{ platform: 'twitch', name: 'thebausffs', live, watchers: [], assigned: 1 },
		{ platform: 'twitch', name: 'caedrel', live, watchers: [], assigned: 1 },
		{ platform: 'kick', name: 'trainwreckstv', live, watchers: [], assigned: 1 }
	];
}

/** Home shows live channels only, at most two, watched ones first. */
export const liveCount = (s: Scene) => all(s).filter((r) => r.live).length;

export function rows(s: Scene): Row[] {
	const live = all(s).filter((r) => r.live);
	return [...live.filter((r) => r.watchers.length), ...live.filter((r) => !r.watchers.length)].slice(0, 2);
}

export const mining = (s: Scene) => (s === 'standby' ? 4 : 3);
