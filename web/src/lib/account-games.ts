import type { AccountGame, AccountGames, Game } from '$lib/api/types';

/** Case- and space-insensitive key, so a typed name matches a listed game. */
const nameKey = (name: string) => name.trim().toLowerCase().replace(/\s+/g, ' ');

/**
 * The list the editor starts from: the stored own list, or a copy of the
 * global list when the account has none yet.
 */
export function startList(own: AccountGames, global: Pick<Game, 'id' | 'name'>[]): AccountGame[] {
	if (own.games.length > 0) return own.games.map((g) => ({ ...g }));
	return global.map((g) => ({ id: g.id, name: g.name }));
}

/** Appends g unless the list has it, by id or by name. Blank names are dropped. */
export function addGame(list: AccountGame[], g: AccountGame): AccountGame[] {
	const name = g.name.trim();
	if (!name) return list;
	const k = nameKey(name);
	if (list.some((x) => (g.id !== '' && x.id === g.id) || nameKey(x.name) === k)) return list;
	return [...list, { id: g.id, name }];
}

/** Whether two lists hold the same games in the same order. */
export function sameList(a: AccountGame[], b: AccountGame[]): boolean {
	return a.length === b.length && a.every((g, i) => g.id === b[i].id && nameKey(g.name) === nameKey(b[i].name));
}
