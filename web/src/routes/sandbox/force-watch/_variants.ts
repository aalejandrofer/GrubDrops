import type { Platform } from '$lib/api/types';

export type Variant = { id: 'a' | 'b'; name: string; note: string };

export const variants: Variant[] = [
	{
		id: 'a',
		name: 'A Add in a dialog',
		note: 'Add channel opens a dialog: paste the URL, the channel and platform show up, tick the accounts, Add. Editing a channel reopens the same dialog.'
	},
	{
		id: 'b',
		name: 'B Add inline in the panel',
		note: 'Add channel turns into a URL field inside the panel. Once the URL parses, account chips for that platform appear under it. Editing a channel expands its row in place.'
	}
];

export type Acct = { id: string; name: string; platform: Platform };
export type Chan = { id: string; platform: Platform; name: string; live: boolean; accounts: string[] };

export const accounts: Acct[] = [
	{ id: 'a1', name: 'TTik3r', platform: 'twitch' },
	{ id: 'a2', name: 'TTik3r', platform: 'kick' },
	{ id: 'a3', name: 'Phluses', platform: 'twitch' },
	{ id: 'a4', name: 'Phluses', platform: 'kick' }
];

export const channels: Chan[] = [
	{ id: 'c1', platform: 'twitch', name: 'keshaeuw', live: true, accounts: ['a1', 'a3'] },
	{ id: 'c2', platform: 'kick', name: 'xqc', live: false, accounts: ['a2'] },
	{ id: 'c3', platform: 'twitch', name: 'thebausffs', live: true, accounts: ['a3'] }
];

/** Mirrors what the real parser would accept: twitch.tv/<name> or kick.com/<name>. */
export function parseChannel(raw: string): { platform: Platform; name: string } | null {
	const s = raw.trim();
	if (!s) return null;
	let u: URL;
	try {
		u = new URL(/^https?:\/\//i.test(s) ? s : `https://${s}`);
	} catch {
		return null;
	}
	const host = u.hostname.replace(/^(www\.|m\.)/, '').toLowerCase();
	const name = u.pathname.split('/').filter(Boolean)[0]?.toLowerCase() ?? '';
	if (!/^[a-z0-9_]{2,25}$/.test(name)) return null;
	if (host === 'twitch.tv') return { platform: 'twitch', name };
	if (host === 'kick.com') return { platform: 'kick', name };
	return null;
}
