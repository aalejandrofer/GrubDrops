import type { Platform } from '$lib/api/types';

export type Variant = { id: 'a' | 'b' | 'c'; name: string; note: string };

export const variants: Variant[] = [
	{
		id: 'a',
		name: 'A Modal',
		note: 'Clicking a campaign on Games, or a reward on Rewards, opens a centered dialog with the whole campaign. Full screen at phone width. Esc or the backdrop closes it.'
	},
	{
		id: 'b',
		name: 'B Side drawer',
		note: 'Same content in a drawer from the right. The page stays visible behind it, so you can click another campaign or reward and the drawer swaps. Full width at phone width.'
	},
	{
		id: 'c',
		name: 'C Inline expand',
		note: 'No overlay. On Games the campaign line expands inside the card. On Rewards the row expands under itself. Several can be open at once.'
	}
];

export type Acct = { id: string; name: string; platform: Platform };

/** claimed | minutes watched | not started | blocked by a missing account link */
export type Cell = { state: 'claimed'; at: string } | { state: 'progress'; min: number } | { state: 'idle' } | { state: 'link' };

export type Drop = { id: string; name: string; minutes: number; cells: Record<string, Cell> };

export type Campaign = {
	id: string;
	platform: Platform;
	game: string;
	name: string;
	starts: string;
	ends: string;
	ended?: boolean;
	linkUrl?: string;
	drops: Drop[];
};

export const accounts: Acct[] = [
	{ id: 'a1', name: 'hJune', platform: 'twitch' },
	{ id: 'a2', name: 'alt_farm', platform: 'twitch' },
	{ id: 'a3', name: 'kick_main', platform: 'kick' }
];

export const campaigns: Campaign[] = [
	{
		id: 'c1',
		platform: 'twitch',
		game: 'Rust',
		name: 'Rust Twitch Drops Round 42',
		starts: 'Oct 23, 20:00',
		ends: 'Oct 30, 20:00',
		drops: [
			{ id: 'd1', name: 'Hazmat Hoodie', minutes: 60, cells: { a1: { state: 'claimed', at: 'Oct 24' }, a2: { state: 'claimed', at: 'Oct 24' } } },
			{ id: 'd2', name: 'Garage Door Skin', minutes: 120, cells: { a1: { state: 'claimed', at: 'Oct 25' }, a2: { state: 'progress', min: 88 } } },
			{ id: 'd3', name: 'Assault Rifle Skin', minutes: 180, cells: { a1: { state: 'progress', min: 34 }, a2: { state: 'idle' } } },
			{ id: 'd4', name: 'Sleeping Bag Skin', minutes: 240, cells: { a1: { state: 'idle' }, a2: { state: 'idle' } } }
		]
	},
	{
		id: 'c2',
		platform: 'kick',
		game: 'Rust',
		name: 'Kick x Rust: Facepunch Week',
		starts: 'Oct 26, 18:00',
		ends: 'Nov 2, 18:00',
		linkUrl: 'https://kick.com/drops/connect',
		drops: [
			{ id: 'd5', name: 'Kick Jacket', minutes: 120, cells: { a3: { state: 'link' } } },
			{ id: 'd6', name: 'Kick Door', minutes: 240, cells: { a3: { state: 'link' } } }
		]
	},
	{
		id: 'c3',
		platform: 'twitch',
		game: 'Marvel Rivals',
		name: 'Season 4 Twitch Drops',
		starts: 'Oct 10, 09:00',
		ends: 'Oct 24, 09:00',
		ended: true,
		drops: [
			{ id: 'd7', name: 'Spray: Grub', minutes: 30, cells: { a1: { state: 'claimed', at: 'Oct 11' } } },
			{ id: 'd8', name: 'Nameplate', minutes: 90, cells: { a1: { state: 'claimed', at: 'Oct 12' } } },
			{ id: 'd9', name: 'Costume', minutes: 240, cells: {} }
		]
	}
];

export type Reward = { campaign: string; drop: string; account: string; at: string };

export const rewards: Reward[] = [
	{ campaign: 'c1', drop: 'd2', account: 'a1', at: 'Oct 25, 14:02' },
	{ campaign: 'c1', drop: 'd1', account: 'a2', at: 'Oct 24, 22:40' },
	{ campaign: 'c1', drop: 'd1', account: 'a1', at: 'Oct 24, 21:13' },
	{ campaign: 'c3', drop: 'd8', account: 'a1', at: 'Oct 12, 18:55' },
	{ campaign: 'c3', drop: 'd7', account: 'a1', at: 'Oct 11, 10:21' }
];
