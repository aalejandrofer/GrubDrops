import type { DiscoverCampaign, Game, Platform } from '$lib/api/types';
import { normName } from '$lib/home';

/** Mirrors Go gameslug.Slug: "Tom's Game!!" -> "toms-game". */
export function gameSlug(name: string): string {
	let out = '';
	for (const ch of name.toLowerCase()) {
		if ((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')) out += ch;
		else if ((ch === ' ' || ch === '-' || ch === '_') && out !== '' && !out.endsWith('-')) out += '-';
	}
	return out.replace(/-+$/, '');
}

/** Name keys a chosen game answers to: its normalized name and slug. */
function chosenKeys(chosen: Pick<Game, 'name' | 'slug'>[]): Set<string> {
	const keys = new Set<string>();
	for (const g of chosen) {
		for (const k of [normName(g.name), normName(g.slug ?? ''), gameSlug(g.name), gameSlug(g.slug ?? '')]) {
			if (k) keys.add(k);
		}
	}
	return keys;
}

/** Whether a campaign game name is already chosen, by name or slug, as the planner matches. */
export function isAdded(chosen: Pick<Game, 'name' | 'slug'>[], name: string): boolean {
	const keys = chosenKeys(chosen);
	return keys.has(normName(name)) || keys.has(gameSlug(name));
}

export interface DiscoverGroup {
	key: string;
	name: string;
	box_art_url: string;
	platforms: Platform[];
	campaigns: DiscoverCampaign[];
	added: boolean;
}

function endMs(c: DiscoverCampaign): number {
	const at = c.ends_at ? Date.parse(c.ends_at) : NaN;
	return Number.isFinite(at) ? at : Infinity;
}

/**
 * Campaigns grouped by game slug, games A to Z, campaigns soonest ending first.
 * Twitch box art wins: it is keyed by the Twitch game id.
 */
export function groupByGame(campaigns: DiscoverCampaign[], chosen: Pick<Game, 'name' | 'slug'>[]): DiscoverGroup[] {
	const keys = chosenKeys(chosen);
	const byKey = new Map<string, DiscoverGroup>();
	for (const c of campaigns) {
		const key = gameSlug(c.game) || normName(c.game);
		if (!key) continue;
		let g = byKey.get(key);
		if (!g) {
			g = {
				key,
				name: c.game,
				box_art_url: c.box_art_url,
				platforms: [],
				campaigns: [],
				added: keys.has(normName(c.game)) || keys.has(key)
			};
			byKey.set(key, g);
		}
		if (c.platform === 'twitch' && !g.platforms.includes('twitch')) {
			g.name = c.game;
			g.box_art_url = c.box_art_url;
		}
		if (!g.platforms.includes(c.platform)) g.platforms.push(c.platform);
		g.campaigns.push(c);
	}
	const out = [...byKey.values()];
	for (const g of out) {
		g.platforms.sort();
		g.campaigns.sort((a, b) => endMs(a) - endMs(b) || a.name.localeCompare(b.name));
	}
	return out.sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }));
}

export interface DiscoverFilter {
	q: string;
	platform: 'all' | Platform;
	hideAdded: boolean;
}

/**
 * Groups matching the filter. The query matches game, campaign or reward
 * names; the platform filter keeps only that platform's campaigns.
 */
export function filterGroups(groups: DiscoverGroup[], f: DiscoverFilter): DiscoverGroup[] {
	const q = normName(f.q);
	const out: DiscoverGroup[] = [];
	for (const g of groups) {
		if (f.hideAdded && g.added) continue;
		let camps = f.platform === 'all' ? g.campaigns : g.campaigns.filter((c) => c.platform === f.platform);
		if (q && !normName(g.name).includes(q)) {
			camps = camps.filter(
				(c) => normName(c.name).includes(q) || c.rewards.some((r) => normName(r.name).includes(q))
			);
		}
		if (camps.length === 0) continue;
		out.push(camps === g.campaigns ? g : { ...g, campaigns: camps, platforms: [...new Set(camps.map((c) => c.platform))].sort() });
	}
	return out;
}

/** Whole minutes until iso, floored at 0; null when there is no end. */
export function minutesLeft(iso: string | null, now: number): number | null {
	if (!iso) return null;
	const at = Date.parse(iso);
	if (!Number.isFinite(at)) return null;
	return Math.max(0, Math.floor((at - now) / 60000));
}
