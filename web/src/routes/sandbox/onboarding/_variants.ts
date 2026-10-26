export type Step = 1 | 2 | 3;

export const steps: { id: Step; name: string }[] = [
	{ id: 1, name: 'Step 1: add an account' },
	{ id: 2, name: 'Step 2: pick games' },
	{ id: 3, name: 'Step 3: mining started' }
];

export type Variant = { id: string; name: string; note: string };

export const variants: Variant[] = [
	{
		id: 'a',
		name: 'A Checklist on Home',
		note: 'Home keeps the app shell. One checklist card: done steps collapse to a summary line, the current step opens. Twitch Android login shown.'
	},
	{
		id: 'b',
		name: 'B Guided wizard',
		note: 'Full screen, no tabs. One task per screen with Back and Continue. Twitch TV code shown. Games list sets priority as you add.'
	},
	{
		id: 'c',
		name: 'C Inline empty states',
		note: 'The real Home dashboard from the start. Empty sections hold the next action in place. Kick cookies.txt shown.'
	}
];

// Static stand-ins. Names follow the fixtures in $lib/fixtures.
export const fake = {
	twitch: 'hJune',
	kick: 'kick_main',
	code: 'WDJB-MJHT',
	activate: 'twitch.tv/activate',
	version: 'v2.0.0',
	hits: [
		{ name: 'Rust', note: 'Twitch, Kick' },
		{ name: 'Rust Console Edition', note: 'Twitch' }
	],
	games: [
		{ name: 'Rust', campaign: 'Rust Drops', platform: 'twitch' as const, drops: 4, ends: 'Nov 2' },
		{ name: 'Marvel Rivals', campaign: 'Season 5 Twitch Drops', platform: 'twitch' as const, drops: 3, ends: 'Nov 9' },
		{ name: 'Apex Legends', campaign: 'Kick Drops Week 2', platform: 'kick' as const, drops: 2, ends: 'Oct 31' }
	],
	runs: [
		{ platform: 'twitch' as const, account: 'hJune', game: 'Rust', drop: 'Rust Reward 1', channel: 'seed_streamer', m: 3, req: 60 },
		{ platform: 'kick' as const, account: 'kick_main', game: 'Apex Legends', drop: 'Apex Legends Reward 1', channel: 'apexpro', m: 1, req: 30 }
	]
};
