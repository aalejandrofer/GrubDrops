import type { Platform } from '$lib/api/types';

export type VariantId = 'now' | 'a' | 'b' | 'c' | 'd';
export type Variant = { id: VariantId; name: string; note: string };

export const variants: Variant[] = [
	{
		id: 'now',
		name: 'Today (for reference)',
		note: 'What prod shows on 2026-10-27. Every idle, paused and signed-out account sits under the rank 1 card, so Phluses and TTik3r look like they wait for Overwatch 2, a Twitch-only campaign that is not on their list.'
	},
	{
		id: 'a',
		name: 'A Separate "Not mining" strip',
		note: 'Game cards list only the accounts mining that game. Idle, paused and signed-out accounts get their own strip under the KPIs, each with its state and the reason it is not mining.'
	},
	{
		id: 'b',
		name: 'B Under the best card it can mine',
		note: 'Each idle account sits under the highest-ranked card that is on its game list and on its platform. An account no card fits goes on a small line under the cards. Needs each account’s game list in GET /api/overview.'
	},
	{
		id: 'c',
		name: 'C One quiet line under the last card',
		note: 'Game cards list only the accounts mining that game. Under the last card, one line of text names every account that is not mining, grouped by state. Names link nowhere; signed out keeps a small Log in link.'
	},
	{
		id: 'd',
		name: 'D Chips under the last card',
		note: 'Same place and grouping as C, as small chips: platform mark, name, a state dot and the state. No Log in link: the signed-out alert at the top already has one. The chips wrap on a phone.'
	}
];

export type GameState = 'mining' | 'waiting' | 'no_campaign';
export type MockGame = {
	id: string;
	name: string;
	rank: number;
	art: string;
	state: GameState;
	camp: string;
	/** Platforms the game's campaigns run on. */
	platforms: Platform[];
	waitLine: string;
};

export type AcctState = 'mining' | 'idle' | 'paused' | 'signed_out';
export type MockAcct = {
	id: string;
	name: string;
	platform: Platform;
	state: AcctState;
	/** Game ids on this account's list. */
	list: string[];
	own: boolean;
	mining?: { game: string; drop: string; channel: string; min: number; req: number; left: string };
};

const art = (id: number) => `https://static-cdn.jtvnw.net/ttv-boxart/${id}_IGDB-144x192.jpg`;

export const games: MockGame[] = [
	{
		id: 'ow',
		name: 'Overwatch 2',
		rank: 1,
		art: art(515025),
		state: 'mining',
		camp: 'Season 20 Twitch Drops',
		platforms: ['twitch'],
		waitLine: ''
	},
	{
		id: 'rust',
		name: 'Rust',
		rank: 2,
		art: art(263490),
		state: 'waiting',
		camp: 'Twitch Rivals BI Generals',
		platforms: ['twitch'],
		waitLine: 'Starts when a drops stream goes live.'
	},
	{
		id: 'lol',
		name: 'League of Legends',
		rank: 3,
		art: art(21779),
		state: 'waiting',
		camp: 'Heartsteel Chat Emote',
		platforms: ['twitch'],
		waitLine: 'Starts when a drops stream goes live.'
	}
];

const shared = ['ow', 'rust', 'lol'];

export const accounts: MockAcct[] = [
	{
		id: 'ryuzec',
		name: 'ryuzec',
		platform: 'twitch',
		state: 'mining',
		list: shared,
		own: false,
		mining: { game: 'ow', drop: 'Mercy Spray', channel: 'keshaeuw', min: 34, req: 60, left: '26m' }
	},
	{ id: 'phl-t', name: 'Phluses', platform: 'twitch', state: 'idle', list: ['rust', 'lol'], own: true },
	{ id: 'ttik3r', name: 'TTik3r', platform: 'kick', state: 'idle', list: ['rust', 'lol'], own: true },
	{ id: 'grubalt', name: 'grubalt', platform: 'twitch', state: 'signed_out', list: shared, own: false },
	{ id: 'dropsbot', name: 'dropsbot', platform: 'kick', state: 'paused', list: shared, own: false }
];

export const nMining = accounts.filter((a) => a.state === 'mining').length;

/** The highest-ranked game on the account's list whose campaigns run on its platform. */
export function bestFit(a: MockAcct): MockGame | null {
	return games.find((g) => a.list.includes(g.id) && g.platforms.includes(a.platform)) ?? null;
}

/** Accounts shown under a card for a variant. */
export function under(v: VariantId, g: MockGame): MockAcct[] {
	return accounts.filter((a) => {
		if (a.mining) return a.mining.game === g.id;
		if (v === 'now') return g.rank === 1;
		if (v === 'b') return bestFit(a)?.id === g.id;
		return false;
	});
}

/** Accounts not mining (variant A strip). */
export const notMining = accounts.filter((a) => a.state !== 'mining');

/** Accounts no card fits (variant B leftover line). */
export const leftover = notMining.filter((a) => bestFit(a) === null);

/** Not-mining accounts grouped by state, in the order C and D show them. */
export const groups: { state: AcctState; accts: MockAcct[] }[] = (['idle', 'signed_out', 'paused'] as AcctState[])
	.map((state) => ({ state, accts: notMining.filter((a) => a.state === state) }))
	.filter((g) => g.accts.length);

export const stateLabel: Record<AcctState, string> = {
	mining: 'Mining',
	idle: 'Idle',
	paused: 'Paused',
	signed_out: 'Signed out'
};

/** Why an account is not mining, for the A strip. */
export function why(a: MockAcct): string {
	if (a.state === 'signed_out') return 'Log in again to mine';
	if (a.state === 'paused') return 'Paused from Accounts';
	const fit = bestFit(a);
	if (!fit) return `No ${a.platform === 'kick' ? 'Kick' : 'Twitch'} campaign on its list`;
	return `Waits for ${fit.name}`;
}

/** Row text for a non-mining account under a card. */
export function rowLine(a: MockAcct, g: MockGame): string {
	if (a.state === 'signed_out') return 'Signed out';
	if (a.state === 'paused') return 'Paused';
	return g.state === 'mining' ? 'Idle' : 'Idle · waits for a drops stream';
}
