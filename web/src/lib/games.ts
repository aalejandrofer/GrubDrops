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

export function campaignLine(c: Campaign, zone: string): string {
	if (!c.ends_at) return tn('games.drops_open', c.drops);
	return tn('games.drops', c.drops, { date: shortDate(c.ends_at, zone) });
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
