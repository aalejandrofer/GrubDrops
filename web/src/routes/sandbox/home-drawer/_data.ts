import type { CampaignDetail, DropCell } from '$lib/api/types';

export type Variant = { id: 'a' | 'b'; name: string; note: string };

export const variants: Variant[] = [
	{
		id: 'a',
		name: 'A. Campaign name is the link',
		note: 'The campaign name on a game card and in an Up next row looks like a link. Click it to open the drawer. From Up next, the clicked drop is highlighted.'
	},
	{
		id: 'b',
		name: 'B. Details button beside the name',
		note: 'Names stay plain text. A small details button sits beside the campaign on the card and in each Up next row, so a click on the row never competes with drag.'
	}
];

const day = 86_400_000;
const iso = (offset: number) => new Date(Date.now() + offset).toISOString();

function cell(name: string, state: DropCell['state'], minutes: number, claimed = false): DropCell {
	return {
		account_id: `acc_${name}`,
		account_name: name,
		platform: 'twitch',
		state,
		reason: '',
		minutes,
		claimed_at: claimed ? iso(-2 * day) : null
	};
}

export const campaigns: Record<string, CampaignDetail> = {
	hachiman: {
		id: 'hachiman',
		platform: 'twitch',
		game: 'SMITE 2',
		name: 'Nov Wk 1 Hachiman!',
		box_art_url: '',
		starts_at: iso(-3 * day),
		ends_at: iso(4 * day),
		live: true,
		link_url: '',
		drops: [
			{ id: 'h1', name: 'Hachiman Avatar', image_url: '', required_minutes: 30, accounts: [cell('hJune', 'claimed', 30, true), cell('grub_alt', 'claimed', 30, true)] },
			{ id: 'h2', name: 'Hachiman Loading Frame', image_url: '', required_minutes: 60, accounts: [cell('hJune', 'accruing', 42), cell('grub_alt', 'accruing', 18)] },
			{ id: 'h3', name: 'Hachiman Skin', image_url: '', required_minutes: 120, accounts: [cell('hJune', 'eligible', 0), cell('grub_alt', 'eligible', 0)] }
		]
	},
	vct: {
		id: 'vct',
		platform: 'twitch',
		game: 'VALORANT',
		name: 'VCT26 GCC Sao Paulo',
		box_art_url: '',
		starts_at: iso(-1 * day),
		ends_at: iso(2 * day),
		live: true,
		link_url: '',
		drops: [
			{ id: 'v1', name: 'Miga Title', image_url: '', required_minutes: 60, accounts: [cell('hJune', 'eligible', 0), cell('grub_alt', 'eligible', 0)] },
			{ id: 'v2', name: 'Sao Paulo Spray', image_url: '', required_minutes: 120, accounts: [cell('hJune', 'eligible', 0), cell('grub_alt', 'eligible', 0)] },
			{ id: 'v3', name: 'GCC Gun Buddy', image_url: '', required_minutes: 240, accounts: [cell('hJune', 'eligible', 0), cell('grub_alt', 'eligible', 0)] }
		]
	}
};

/** Up next rows: drop, its campaign, and the account it queues on. */
export const queue = [
	{ drop: 'h3', name: 'Hachiman Skin', game: 'SMITE 2', camp: 'hachiman', min: 120 },
	{ drop: 'v1', name: 'Miga Title', game: 'VALORANT', camp: 'vct', min: 60 },
	{ drop: 'v2', name: 'Sao Paulo Spray', game: 'VALORANT', camp: 'vct', min: 120 },
	{ drop: 'v3', name: 'GCC Gun Buddy', game: 'VALORANT', camp: 'vct', min: 240 }
];
