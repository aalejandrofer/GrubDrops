import type { Platform } from '$lib/api/types';

export type VariantId = 'now' | 'a' | 'b' | 'c' | 'd' | 'e' | 'f';
export type Variant = { id: VariantId; name: string; note: string };

export const variants: Variant[] = [
	{
		id: 'now',
		name: 'Today (for reference)',
		note: 'Priority label, hint and Cards | List on the first row. Drop priority label and its switch on a second row, switch pushed to the right.'
	},
	{
		id: 'a',
		name: 'A Drop priority row above Priority',
		note: 'The Drop priority label and its switch move to the top left of the list area, as their own row above the Priority row. Switch sits right after the label, not at the far right.'
	},
	{
		id: 'b',
		name: 'B One header row',
		note: 'Drop priority switch at the top left, Cards | List at the top right, on one row. The "Top game mines first." hint sits under the switch. The Priority heading is gone: the switch label says what the list does.'
	},
	{
		id: 'c',
		name: 'C Next to the page title',
		note: 'Drop priority becomes a page-level control: label and switch sit next to the Games title at the top left. The Priority row keeps only the hint and Cards | List. Scan now stays top right.'
	},
	{
		id: 'd',
		name: 'D Own panel at the top of the rail',
		note: 'Round 2. A Drop priority panel sits above Account lists in the left rail, styled like the other rail panels. The switch has two equal segments across the rail, with a one-line hint under it that follows the mode. Above the first card the main column holds only the Add a game search and one row: the list hint on the left, Cards | List on the right. On a phone the rail stacks above the list, as on the real page.'
	},
	{
		id: 'e',
		name: 'E Switch inside the Account lists panel',
		note: 'Round 2. No separate panel: a compact segmented control sits at the top of the Account lists panel, above its heading, with the hint under it. Same short main column as D: search, then one row with the hint and Cards | List. The rail stacks above the list on a phone.'
	},
	{
		id: 'f',
		name: 'F Chosen: switch in its own rail box, Cards | List in the header',
		note: 'Round 3, Jandro\'s pick. The switch sits alone in its own small panel at the top of the rail, above Account lists and Force watch, titled Priority Mode like the other rail panels, with no hint. Cards | List moves to the page header, just left of Scan now. The main column is the Add a game search and then the cards. Ending Soonest shows through the clock grips and the re-sorted order only.'
	}
];

export type MockGame = {
	id: string;
	name: string;
	art: string;
	state: 'mining' | 'waiting' | 'no_campaign';
	/** Hours until the first campaign ends. */
	endsIn: number;
	camps: { platform: Platform; name: string; line: string }[];
};

const art = (id: number) => `https://static-cdn.jtvnw.net/ttv-boxart/${id}_IGDB-144x192.jpg`;

export const games: MockGame[] = [
	{
		id: 'ow',
		name: 'Overwatch 2',
		art: art(515025),
		state: 'mining',
		endsIn: 190,
		camps: [{ platform: 'twitch', name: 'Season 20 Twitch Drops', line: '3 drops · ends in 7d' }]
	},
	{
		id: 'rust',
		name: 'Rust',
		art: art(263490),
		state: 'waiting',
		endsIn: 30,
		camps: [
			{ platform: 'twitch', name: 'Twitch Rivals BI Generals', line: '2 drops · ends in 1d' },
			{ platform: 'kick', name: 'Rust Kick Drops', line: '4 drops · ends in 5d' }
		]
	},
	{
		id: 'lol',
		name: 'League of Legends',
		art: art(21779),
		state: 'waiting',
		endsIn: 70,
		camps: [{ platform: 'twitch', name: 'Heartsteel Chat Emote', line: '1 drop · ends in 2d' }]
	},
	{
		id: 'gta',
		name: 'Grand Theft Auto V',
		art: art(32982),
		state: 'no_campaign',
		endsIn: Infinity,
		camps: []
	},
	{
		id: 'slots',
		name: 'Slots',
		art: art(498566),
		state: 'waiting',
		endsIn: 12,
		camps: [{ platform: 'kick', name: 'Slots Weekend', line: '2 drops · ends in 12h' }]
	}
];

export const byEnding = (list: MockGame[]): MockGame[] => [...list].sort((a, b) => a.endsIn - b.endsIn);

export const stateText: Record<MockGame['state'], string> = {
	mining: 'Mining',
	waiting: 'Waiting',
	no_campaign: 'No campaign'
};
