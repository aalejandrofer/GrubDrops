export type VariantId = 'before' | 'a' | 'b' | 'c';
export type Variant = { id: VariantId; name: string; note: string };

export const variants: Variant[] = [
	{
		id: 'before',
		name: 'Before (today)',
		note: 'Three stacked pieces under the search: an "e.g." link, a "Campaign missing?" toggle and its paragraph. The other spots show their help text as it is now.'
	},
	{
		id: 'a',
		name: 'A Hint line, examples on the next line',
		note: 'One line of hint, then the Twitch and Kick links on their own line, each tagged with its platform mark. No toggle. Text-only hints (Points, cookies.txt) are the same component with no examples.'
	},
	{
		id: 'b',
		name: 'B Hint and examples on one line',
		note: 'Same pieces as A on one line. On a phone the examples wrap under the hint, so it ends up like A there. Shortest block on desktop.'
	},
	{
		id: 'c',
		name: 'C Chosen: examples fill the field',
		note: 'Jandro\'s pick, round 2. Add a game keeps only the two tagged examples, no text. Click Kick: the box gets https://kick.com/category/rust with "rust" selected, so typing the game replaces it. Points has no hint. cookies.txt links to the Get cookies.txt extension. Proxy examples fill the scheme.'
	}
];

export const TWITCH_CAT = 'https://www.twitch.tv/directory/category/rust';
export const KICK_CAT = 'https://kick.com/category/rust';
export const ANDROID_EXAMPLE = 'https://www.twitch.tv/#access_token=XXX&scope=&token_type=bearer';
export const COOKIES_EXT = 'https://chromewebstore.google.com/detail/get-cookiestxt-locally/cclelndahbckbenkjhflpdbgdldlbecc?hl=en';
