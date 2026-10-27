import type { Platform, Runner, Target } from '$lib/api/types';

export type Queue = {
	accountId: string;
	accountName: string;
	platform: Platform;
	items: Target[];
	custom: boolean;
	/** The runner can switch to an Up next item now. */
	canMine: boolean;
};

/** A signed-out or paused account cannot switch drops. */
export function canMineNow(r: Pick<Runner, 'state'>): boolean {
	return r.state !== 'needs_login' && r.state !== 'no_games' && r.state !== 'idle';
}

/** New array with the item at `from` moved to `to`. Out-of-range indexes return a plain copy. */
export function moveItem<T>(list: readonly T[], from: number, to: number): T[] {
	const out = list.slice();
	const ok = (i: number) => Number.isInteger(i) && i >= 0 && i < list.length;
	if (!ok(from) || !ok(to) || from === to) return out;
	const [x] = out.splice(from, 1);
	out.splice(to, 0, x);
	return out;
}

/** One queue per runner that has something up next, in runner order. */
export function queues(runners: Runner[]): Queue[] {
	return runners
		.filter((r) => r.up_next.length > 0)
		.map((r) => ({
			accountId: r.account_id,
			accountName: r.account_name,
			platform: r.platform,
			items: r.up_next,
			custom: r.order_custom,
			canMine: canMineNow(r)
		}));
}

/** The drop an item follows: the current drop for item 0, else the previous item. */
export function afterLabel(r: Runner, index: number): string | null {
	if (index === 0) return r.current?.benefit_name ?? null;
	return r.up_next[index - 1]?.benefit_name ?? null;
}

export function anyCustom(runners: Runner[]): boolean {
	return runners.some((r) => r.order_custom);
}

export interface OrderIO {
	get(): Runner[];
	set(runners: Runner[]): void;
	setOrder(accountId: string, benefitIds: string[]): Promise<void>;
	fetchRunners(): Promise<Runner[]>;
}

/**
 * Apply a move locally, save it, then replace the list with the server's.
 * On failure the server list wins; if that fetch also fails, the previous list is restored.
 * Returns true when the save succeeded.
 */
export async function commitMove(io: OrderIO, accountId: string, from: number, to: number): Promise<boolean> {
	const before = io.get();
	const r = before.find((x) => x.account_id === accountId);
	if (!r) return true;
	const next = moveItem(r.up_next, from, to);
	if (next.every((x, i) => x === r.up_next[i])) return true;
	io.set(before.map((x) => (x.account_id === accountId ? { ...x, up_next: next, order_custom: true } : x)));
	let ok = true;
	try {
		await io.setOrder(accountId, next.map((x) => x.benefit_id));
	} catch {
		ok = false;
	}
	try {
		io.set(await io.fetchRunners());
	} catch {
		if (!ok) io.set(before);
	}
	return ok;
}

/** The queue Mine now acts on: the first account that can switch and has items. */
export function mineTarget(qs: readonly Queue[]): Queue | null {
	return qs.find((q) => q.canMine && q.items.length > 0) ?? null;
}

/** The runner after Mine now: item 0 becomes current, the old current leads Up next. */
export function applyMineNow(r: Runner): Runner {
	const [first, ...rest] = r.up_next;
	if (!first) return r;
	return {
		...r,
		current: first,
		minutes: first.minutes,
		channel: '',
		viewer_count: 0,
		up_next: r.current ? [r.current, ...rest] : rest,
		order_custom: true
	};
}

export interface MineIO {
	get(): Runner[];
	set(runners: Runner[]): void;
	mineNow(accountId: string): Promise<unknown>;
	fetchRunners(): Promise<Runner[]>;
}

/**
 * Show the switch at once, then ask the server. The runner event that follows
 * carries the real state. On failure the server list wins; if that fetch also
 * fails, the previous list is restored. Returns true when the request succeeded.
 */
export async function commitMineNow(io: MineIO, accountId: string): Promise<boolean> {
	const before = io.get();
	if (!before.some((x) => x.account_id === accountId && x.up_next.length > 0)) return false;
	io.set(before.map((x) => (x.account_id === accountId ? applyMineNow(x) : x)));
	try {
		await io.mineNow(accountId);
		return true;
	} catch {
		try {
			io.set(await io.fetchRunners());
		} catch {
			io.set(before);
		}
		return false;
	}
}

/** Rows of one queue to render: the first `limit` unless expanded, and how many more there are. */
export function foldQueue<T>(items: readonly T[], expanded: boolean, limit = 5): { rows: T[]; more: number } {
	const more = Math.max(0, items.length - limit);
	return { rows: expanded || more === 0 ? items.slice() : items.slice(0, limit), more };
}

export type GroupDrop = { target: Target; accountId: string; accountName: string; platform: Platform };

export type GroupCampaign = {
	id: string;
	platform: Platform;
	name: string;
	endsAt: string | null;
	drops: GroupDrop[];
};

export type GameGroup = {
	game: string;
	campaigns: GroupCampaign[];
	/** Accounts with this game queued, in runner order. */
	accounts: { id: string; name: string; platform: Platform }[];
	/** Queued drops summed over every campaign and account. */
	drops: number;
	/** Watch minutes still needed for every queued drop. */
	minutesLeft: number;
};

/**
 * Up next folded to one entry per game. A game's place is the earliest queue
 * position any account has for it; ties go to runner order. Campaigns keep the
 * same order. Drops are listed per account in runner order, each in queue order.
 */
export function byGame(qs: readonly Queue[]): GameGroup[] {
	const groups = new Map<string, GameGroup>();
	const depth = qs.reduce((n, q) => Math.max(n, q.items.length), 0);
	for (let i = 0; i < depth; i++) {
		for (const q of qs) {
			const x = q.items[i];
			if (!x) continue;
			let g = groups.get(x.game);
			if (!g) {
				g = { game: x.game, campaigns: [], accounts: [], drops: 0, minutesLeft: 0 };
				groups.set(x.game, g);
			}
			let c = g.campaigns.find((c) => c.platform === x.platform && c.id === x.campaign_id);
			if (!c) {
				c = { id: x.campaign_id, platform: x.platform, name: x.campaign_name, endsAt: x.ends_at, drops: [] };
				g.campaigns.push(c);
			}
			c.drops.push({ target: x, accountId: q.accountId, accountName: q.accountName, platform: q.platform });
			if (!g.accounts.some((a) => a.id === q.accountId))
				g.accounts.push({ id: q.accountId, name: q.accountName, platform: q.platform });
			g.drops++;
			g.minutesLeft += Math.max(0, x.required_minutes - x.minutes);
		}
	}
	const order = new Map(qs.map((q, i) => [q.accountId, i]));
	for (const g of groups.values()) {
		g.accounts.sort((a, b) => order.get(a.id)! - order.get(b.id)!);
		for (const c of g.campaigns) c.drops.sort((a, b) => order.get(a.accountId)! - order.get(b.accountId)!);
	}
	return [...groups.values()];
}

/**
 * Runners with each queue stable-sorted by the game order `order`, so drops of
 * one game keep their order. A game missing from `order` keeps its relative
 * place at the end. A runner whose queue comes out the same is returned as the
 * same object.
 */
export function reorderByGame(runners: Runner[], order: readonly string[]): Runner[] {
	const rank = new Map(order.map((g, i) => [g, i]));
	const at = (g: string) => rank.get(g) ?? order.length;
	return runners.map((r) => {
		const next = r.up_next
			.map((x, i) => ({ x, i }))
			.sort((a, b) => at(a.x.game) - at(b.x.game) || a.i - b.i)
			.map((e) => e.x);
		if (next.every((x, i) => x === r.up_next[i])) return r;
		return { ...r, up_next: next, order_custom: true };
	});
}

/**
 * Sort every queue to the game order `order`: apply it locally, save every
 * runner whose queue changed, then replace the list with the server's. On
 * failure the server list wins; if that fetch also fails, the previous list is
 * restored. Returns true when every save succeeded.
 */
export async function commitGameOrder(io: OrderIO, order: readonly string[]): Promise<boolean> {
	const before = io.get();
	const next = reorderByGame(before, order);
	const changed = next.filter((r, i) => r !== before[i]);
	if (changed.length === 0) return true;
	io.set(next);
	let ok = true;
	try {
		await Promise.all(
			changed.map((r) =>
				io.setOrder(
					r.account_id,
					r.up_next.map((x) => x.benefit_id)
				)
			)
		);
	} catch {
		ok = false;
	}
	try {
		io.set(await io.fetchRunners());
	} catch {
		if (!ok) io.set(before);
	}
	return ok;
}

/**
 * Groups in the dropped game order. Games not in `order` follow in their
 * derived order; games in `order` that are gone are ignored.
 */
export function inGameOrder(groups: readonly GameGroup[], order: readonly string[] | null): GameGroup[] {
	if (!order || order.length === 0) return groups.slice();
	const rank = new Map(order.map((g, i) => [g, i]));
	const at = (g: string) => rank.get(g) ?? order.length;
	return groups
		.map((g, i) => ({ g, i }))
		.sort((a, b) => at(a.g.game) - at(b.g.game) || a.i - b.i)
		.map((e) => e.g);
}

/**
 * The game order Inventory shows after a drag. `custom` is true once a runner
 * had a custom order while it was set, so a later reset, here or on Home,
 * clears it.
 */
export type GameOrder = { games: string[]; custom: boolean };

export const GAME_ORDER_KEY = 'grubdrops.upnext.game_order';

/**
 * The stored order after the runners change: null when the queues went back
 * to the Games order, else marked custom once any runner is. An empty runner
 * list means not loaded yet and keeps the order.
 */
export function settleGameOrder(o: GameOrder | null, runners: Runner[]): GameOrder | null {
	if (!o || runners.length === 0) return o;
	const custom = anyCustom(runners);
	if (o.custom && !custom) return null;
	if (custom && !o.custom) return { ...o, custom: true };
	return o;
}

type KV = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;

function session(): KV | null {
	try {
		return sessionStorage;
	} catch {
		return null;
	}
}

export function readGameOrder(store: KV | null = session()): GameOrder | null {
	try {
		const v = JSON.parse(store?.getItem(GAME_ORDER_KEY) ?? 'null');
		if (!v || !Array.isArray(v.games) || !v.games.every((g: unknown) => typeof g === 'string')) return null;
		return { games: v.games, custom: v.custom === true };
	} catch {
		return null;
	}
}

export function writeGameOrder(o: GameOrder | null, store: KV | null = session()): void {
	try {
		if (o) store?.setItem(GAME_ORDER_KEY, JSON.stringify(o));
		else store?.removeItem(GAME_ORDER_KEY);
	} catch {
		/* storage unavailable: the order lasts until the page reloads */
	}
}

/** Runners with one account's queued drop removed, e.g. after it is marked collected. */
export function withoutDrop(runners: Runner[], accountId: string, benefitId: string): Runner[] {
	return runners.map((r) =>
		r.account_id === accountId && r.up_next.some((x) => x.benefit_id === benefitId)
			? { ...r, up_next: r.up_next.filter((x) => x.benefit_id !== benefitId) }
			: r
	);
}
