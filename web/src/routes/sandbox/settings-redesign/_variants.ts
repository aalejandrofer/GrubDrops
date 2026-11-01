export type Variant = { id: 'colorful' | 'sidebar' | 'hub' | 'scroll'; name: string; line: string; note: string; tradeoff: string };

export const variants: Variant[] = [
	{
		id: 'colorful',
		name: 'Colorful pass of today’s page',
		line: 'Same eight sections and scroll index, now colour-coded.',
		note: 'Each section gets a hue and an icon tile, used on its nav link, heading, switches and focus rings. State chips colour the headings (needs login, canary failing, up to date) and the nav shows a dot when a section needs you. Twitch purple and Kick green mark accounts. Phone: the index becomes a row of coloured pills.',
		tradeoff: 'Tradeoff: cheapest to ship and easier to scan, but the page is still one long scroll; finding a setting still means knowing its section.'
	},
	{
		id: 'sidebar',
		name: 'A · Sidebar with groups and search',
		line: 'Four groups, 11 smaller sections, one shown at a time, plus search.',
		note: 'Engine and Notifications split into smaller sections under Accounts, Mining, Notifications and System. One section shows at a time. Search (press /) lists matching settings with their section; picking one opens the section and flashes the row. Phone: the sidebar becomes the Settings home list with a live summary per row; a section opens as its own page with a back link.',
		tradeoff: 'Tradeoff: best findability and room to grow, but no single page to skim everything, and one more tap on phone.'
	},
	{
		id: 'hub',
		name: 'B · Overview hub of cards',
		line: 'A grid of category cards with live one-line summaries and quick switches.',
		note: 'The landing view answers "is everything OK?" before any form: a Needs you strip (expired login, failing canary), then one card per section with a live summary ("2 Twitch, 2 Kick · 1 needs login", "Discord on", "01:00 to 08:00"). Sleep, Experimental and Updates flip right on the card. A card opens its section with sibling tabs for the group. Phone: cards turn into list rows.',
		tradeoff: 'Tradeoff: great status at a glance and friendly on phone, but every edit is one click deeper and the hub can drift into a second dashboard.'
	},
	{
		id: 'scroll',
		name: 'C · One page, grouped cards, unsaved bar',
		line: 'Everything on one page in colour bands, with a sticky index and a Save bar.',
		note: 'All 11 sections stay on one page, banded by group with a coloured header, each section in its own card. A sticky index tracks the scroll. Switches still save at once; typed fields (times, URLs, IDs) wait and collect in an unsaved bar with Discard and Save. One pending edit (Sleep start) is pre-filled so the bar shows. Phone: the index folds into a sticky "jump to" button that opens a section list; it closes on outside click or Escape.',
		tradeoff: 'Tradeoff: keeps skim-everything and fixes the "did it save?" doubt, but the page keeps getting longer and mixes two save models.'
	}
];
