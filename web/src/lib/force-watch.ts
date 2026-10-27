import type { ForceChannel, ForceChannelPut, Platform, Runner } from '$lib/api/types';

// Same rules as the server: Kick slugs may also hold a hyphen.
const NAME: Record<Platform, RegExp> = {
	twitch: /^[a-z0-9_]{2,25}$/,
	kick: /^[a-z0-9_-]{2,25}$/
};

/** A twitch.tv/<name> or kick.com/<name> link, with or without the scheme. */
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
	const platform: Platform | null = host === 'twitch.tv' ? 'twitch' : host === 'kick.com' ? 'kick' : null;
	if (!platform || !NAME[platform].test(name)) return null;
	return { platform, name };
}

/** New list with item i swapped with its neighbour d (-1 up, 1 down). Out of range returns a plain copy. */
export function moveChannel<T>(list: readonly T[], i: number, d: number): T[] {
	const out = list.slice();
	const j = i + d;
	if (i < 0 || i >= list.length || j < 0 || j >= list.length) return out;
	[out[i], out[j]] = [out[j], out[i]];
	return out;
}

/**
 * ids still in known. A deleted account leaves its id behind until the list
 * reloads, and the server rejects unknown ids. An empty known set (accounts
 * not loaded yet) keeps every id.
 */
export function knownIds(ids: readonly string[], known: ReadonlySet<string>): string[] {
	return known.size === 0 ? [...ids] : ids.filter((id) => known.has(id));
}

/** The PUT body: only the fields the server accepts, with unknown account ids dropped. */
export function toPut(list: readonly ForceChannel[], known: ReadonlySet<string> = new Set()): ForceChannelPut[] {
	return list.map((c) => ({ platform: c.platform, channel: c.channel, accounts: knownIds(c.accounts, known) }));
}

export function togglePick(picked: readonly string[], id: string): string[] {
	return picked.includes(id) ? picked.filter((x) => x !== id) : [...picked, id];
}

/** Accounts force watching c now. Runner pushes are newer than the list, so they win once loaded. */
export function watchingIds(c: ForceChannel, runners: readonly Runner[]): string[] {
	if (runners.length === 0) return c.watching;
	return runners
		.filter((r) => r.state === 'force_watching' && r.platform === c.platform && r.channel.toLowerCase() === c.channel)
		.map((r) => r.account_id);
}
