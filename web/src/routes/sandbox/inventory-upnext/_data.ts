import type { Platform } from '$lib/api/types';

export type Variant = { id: 'a' | 'b'; name: string; note: string };

export const variants: Variant[] = [
	{
		id: 'a',
		name: 'A Inline expand',
		note: 'One row per game, in queue order, with the drops of all its campaigns added up. Click a row to open it in place: each campaign, then its drops in mining order. Several rows can be open at once.'
	},
	{
		id: 'b',
		name: 'B Side drawer',
		note: 'Same one row per game. Click a row and the campaign drawer slides in from the right with every campaign of that game. Click another row and the drawer swaps. Full width at phone width.'
	}
];

export type Drop = { name: string; minutes: number; watched: number; account: string };

export type Campaign = { id: string; platform: Platform; name: string; ends: string; drops: Drop[] };

export type Game = { name: string; art: string | null; accounts: string[]; campaigns: Campaign[] };

/** Up next across all accounts, in queue order: the first drop of each game decides its place. */
export const games: Game[] = [
	{
		name: 'War Thunder',
		art: 'https://static-cdn.jtvnw.net/ttv-boxart/66366_IGDB-144x192.jpg',
		accounts: ['hJune', 'alt_farm'],
		campaigns: [
			{
				id: 'wt1',
				platform: 'twitch',
				name: 'Nov Wk 1 Ground Forces',
				ends: 'Nov 3, 19:00',
				drops: [
					{ name: 'Decal: Grub Tank', minutes: 60, watched: 42, account: 'hJune' },
					{ name: 'Camo: Autumn', minutes: 120, watched: 0, account: 'hJune' },
					{ name: 'Title: Drop Hunter', minutes: 180, watched: 0, account: 'hJune' },
					{ name: 'Decal: Grub Tank', minutes: 60, watched: 0, account: 'alt_farm' },
					{ name: 'Camo: Autumn', minutes: 120, watched: 0, account: 'alt_farm' },
					{ name: 'Title: Drop Hunter', minutes: 180, watched: 0, account: 'alt_farm' }
				]
			},
			{
				id: 'wt2',
				platform: 'twitch',
				name: 'Tanks Anniversary',
				ends: 'Nov 9, 19:00',
				drops: [
					{ name: 'Premium Day', minutes: 30, watched: 0, account: 'hJune' },
					{ name: 'Golden Eagles x50', minutes: 90, watched: 0, account: 'hJune' },
					{ name: 'Premium Day', minutes: 30, watched: 0, account: 'alt_farm' },
					{ name: 'Golden Eagles x50', minutes: 90, watched: 0, account: 'alt_farm' }
				]
			}
		]
	},
	{
		name: 'SMITE 2',
		art: null,
		accounts: ['hJune'],
		campaigns: [
			{
				id: 'sm1',
				platform: 'twitch',
				name: 'Season 2 Launch',
				ends: 'Nov 5, 17:00',
				drops: [
					{ name: 'Avatar: Ra', minutes: 60, watched: 0, account: 'hJune' },
					{ name: 'Ward Skin', minutes: 120, watched: 0, account: 'hJune' },
					{ name: 'Jump Stamp', minutes: 180, watched: 0, account: 'hJune' },
					{ name: 'Gems x200', minutes: 240, watched: 0, account: 'hJune' }
				]
			}
		]
	},
	{
		name: 'Rust',
		art: 'https://static-cdn.jtvnw.net/ttv-boxart/263490_IGDB-144x192.jpg',
		accounts: ['kick_main'],
		campaigns: [
			{
				id: 'ru1',
				platform: 'kick',
				name: 'Kick x Rust: Facepunch Week',
				ends: 'Nov 2, 18:00',
				drops: [
					{ name: 'Kick Jacket', minutes: 120, watched: 15, account: 'kick_main' },
					{ name: 'Kick Door', minutes: 240, watched: 0, account: 'kick_main' }
				]
			}
		]
	},
	{
		name: 'VALORANT',
		art: 'https://static-cdn.jtvnw.net/ttv-boxart/516575_IGDB-144x192.jpg',
		accounts: ['alt_farm'],
		campaigns: [
			{
				id: 'va1',
				platform: 'twitch',
				name: 'VCT26 GCC Sao Paulo',
				ends: 'Nov 12, 22:00',
				drops: [
					{ name: 'Spray: Sao Paulo', minutes: 60, watched: 0, account: 'alt_farm' },
					{ name: 'Player Card', minutes: 120, watched: 0, account: 'alt_farm' },
					{ name: 'Gun Buddy', minutes: 180, watched: 0, account: 'alt_farm' }
				]
			}
		]
	}
];

export const accountPlatform: Record<string, Platform> = { hJune: 'twitch', alt_farm: 'twitch', kick_main: 'kick' };

export const dropCount = (g: Game) => g.campaigns.reduce((n, c) => n + c.drops.length, 0);

/** Minutes still to watch across every queued drop of the game. */
export const minutesLeft = (g: Game) => g.campaigns.reduce((n, c) => n + c.drops.reduce((m, d) => m + d.minutes - d.watched, 0), 0);

export const hours = (min: number) => (min < 60 ? `${min} min` : `${Math.round(min / 6) / 10} h`);
