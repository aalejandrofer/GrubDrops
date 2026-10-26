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

/** Rows of one queue to render: the first `limit` unless expanded, and how many more there are. */
export function foldQueue<T>(items: readonly T[], expanded: boolean, limit = 5): { rows: T[]; more: number } {
	const more = Math.max(0, items.length - limit);
	return { rows: expanded || more === 0 ? items.slice() : items.slice(0, limit), more };
}
