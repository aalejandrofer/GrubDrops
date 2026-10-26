import type { Platform, Runner, Target } from '$lib/api/types';

export type Queue = {
	accountId: string;
	accountName: string;
	platform: Platform;
	items: Target[];
	custom: boolean;
};

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
			custom: r.order_custom
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
