export type Variant = { id: 'a' | 'b' | 'c'; name: string; note: string };

export const variants: Variant[] = [
	{
		id: 'a',
		name: 'A Section below the list',
		note: 'Games page as today, plus a "Per-account lists" section under the priority list. Each account row has the switch; turning it on opens the list in a modal. Edit list reopens it.'
	},
	{
		id: 'b',
		name: 'B Left rail, modal',
		note: 'A narrow rail on the left holds the accounts and their switches. The main column stays the shared list, untouched. The list itself opens in the same modal as A.'
	},
	{
		id: 'c',
		name: 'C Left rail, swaps the list',
		note: 'The rail picks whose list the page shows. Shared list is the default. Picking an account on its own list shows that list in the main column with the same cards, search and reorder. No modal.'
	}
];

export type Platform = 'twitch' | 'kick';
export type Acct = { id: string; name: string; platform: Platform; own: boolean; games: string[] };

export const shared = ['Rust', 'Marvel Rivals', 'Apex Legends', 'Delta Force'];

export const accounts: Acct[] = [
	{ id: 'a1', name: 'hJune', platform: 'twitch', own: false, games: [] },
	{ id: 'a2', name: 'kick_main', platform: 'kick', own: true, games: ['Apex Legends', 'Rust'] },
	{ id: 'a3', name: 'alt_farm', platform: 'twitch', own: true, games: ['Delta Force', 'Marvel Rivals', 'Rust'] },
	{ id: 'a4', name: 'sidekick', platform: 'kick', own: false, games: [] }
];
