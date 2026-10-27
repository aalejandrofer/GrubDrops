import { ApiError } from '$lib/api/client';
import type { Campaign, Game, SearchHit } from '$lib/api/types';
import { shortDate } from '$lib/format';
import { normName } from '$lib/home';
import { tn } from '$lib/i18n/index.svelte';
import { moveItem } from '$lib/upnext';

export function withoutId(ids: string[], id: string): string[] {
	return ids.filter((x) => x !== id);
}

export type Suggestion = { kind: 'hit'; hit: SearchHit } | { kind: 'add'; name: string };

/** Search hits first, then an add entry for the typed text when no hit matches it exactly. */
export function suggestions(hits: SearchHit[], q: string): Suggestion[] {
	const out: Suggestion[] = hits.map((hit) => ({ kind: 'hit', hit }));
	const name = q.trim();
	if (name && !hits.some((h) => normName(h.name) === normName(name))) out.push({ kind: 'add', name });
	return out;
}

/** Splits a name around the first case-insensitive match of the trimmed query, for highlighting. */
export function matchParts(name: string, q: string): { pre: string; hit: string; post: string } {
	const needle = q.trim().toLowerCase();
	const lower = name.toLowerCase();
	// Lowercasing can change the length (e.g. "İ"); the offsets would then be wrong.
	const i = needle && lower.length === name.length ? lower.indexOf(needle) : -1;
	if (i < 0) return { pre: name, hit: '', post: '' };
	return { pre: name.slice(0, i), hit: name.slice(i, i + needle.length), post: name.slice(i + needle.length) };
}

export function campaignLine(c: Campaign, zone: string): string {
	if (!c.ends_at) return tn('games.drops_open', c.drops);
	return tn('games.drops', c.drops, { date: shortDate(c.ends_at, zone) });
}

/** The URL when it is absolute http or https, else ''. */
export function linkHref(url: string | null | undefined): string {
	if (!url) return '';
	try {
		const u = new URL(url);
		return u.protocol === 'https:' || u.protocol === 'http:' ? url : '';
	} catch {
		return '';
	}
}

/** Distinct names of accounts that must link to claim the game's drops, in server order. */
export function linkNames(g: Pick<Game, 'needs_link'>): string[] {
	return [...new Set((g.needs_link ?? []).map((n) => n.account_name))];
}

/** What a link recheck found across the accounts it ran for. */
export type RecheckOutcome = 'claimed' | 'still' | 'linked' | 'failed';

export interface RecheckResult {
	outcome: RecheckOutcome;
	claimed: number;
}

/**
 * Recheck each account's link-refused claims and fold the results: any claim
 * wins, then any account still unlinked, then a failure, else linked.
 */
export async function recheckAccounts(
	ids: string[],
	call: (id: string) => Promise<{ claimed: number; still_needs_link: number }>
): Promise<RecheckResult> {
	let claimed = 0;
	let still = 0;
	let failed = 0;
	for (const id of new Set(ids)) {
		try {
			const r = await call(id);
			claimed += r.claimed;
			still += r.still_needs_link;
		} catch {
			failed++;
		}
	}
	const outcome: RecheckOutcome = claimed > 0 ? 'claimed' : still > 0 ? 'still' : failed > 0 ? 'failed' : 'linked';
	return { outcome, claimed };
}

export interface GamesIO {
	get(): Game[];
	set(games: Game[]): void;
	setGames(ids: string[]): Promise<void>;
	fetchGames(): Promise<Game[]>;
}

/**
 * Apply a new order locally, save the full id list, then take the server's list.
 * On failure the server list wins; if that fetch also fails, the previous list is restored.
 * Returns true when the save succeeded.
 */
export async function commitGames(io: GamesIO, next: Game[]): Promise<boolean> {
	const before = io.get();
	if (next.length === before.length && next.every((g, i) => g.id === before[i].id)) return true;
	io.set(next);
	let ok = true;
	try {
		await io.setGames(next.map((g) => g.id));
	} catch {
		ok = false;
	}
	try {
		io.set(await io.fetchGames());
	} catch {
		if (!ok) io.set(before);
	}
	return ok;
}

export function moveGame(games: Game[], from: number, to: number): Game[] {
	return moveItem(games, from, to);
}

/** Games in the order Ending Soonest mines them: earliest campaign end first, game order for ties. Games with no end date go last. */
export function byEndingSoonest(games: Game[]): Game[] {
	const end = (g: Game) => Math.min(...g.campaigns.map((c) => (c.ends_at ? Date.parse(c.ends_at) : Infinity)), Infinity);
	return games
		.map((g, i) => ({ g, i, at: end(g) }))
		.sort((a, b) => (a.at === b.at ? a.i - b.i : a.at - b.at))
		.map((x) => x.g);
}

/** Target index for a move button: one step, or the very top/bottom with Shift. */
export function moveTarget(len: number, i: number, dir: -1 | 1, toEnd: boolean): number {
	const t = toEnd ? (dir < 0 ? 0 : len - 1) : i + dir;
	return Math.max(0, Math.min(len - 1, t));
}

/** The existing priority when an add failed because the game is already chosen, else null. */
export function dupePriority(e: unknown): number | null {
	return e instanceof ApiError && e.status === 409 && typeof e.body.priority === 'number' ? e.body.priority : null;
}

const linkErrors: Record<string, string> = {
	unknown_category: 'games.link_unknown',
	lookup_failed: 'games.link_failed',
	no_account: 'games.link_no_account'
};

/** The message key when adding a pasted category link failed, else null. */
export function linkErrorKey(e: unknown): string | null {
	return e instanceof ApiError ? (linkErrors[e.code] ?? null) : null;
}
