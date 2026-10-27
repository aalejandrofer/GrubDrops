import type { ClaimsQuery, Runner, Target } from '$lib/api/types';

export const PAGE_SIZES = [20, 50, 100] as const;
export const DEFAULT_PAGE_SIZE = 20;
export const PAGE_SIZE_KEY = 'grub.rewards.page_size';

export type InProgress = { target: Target; runner: Runner; pct: number; left: number };

/** Drops being mined now, furthest along first. */
export function inProgress(r: Runner[]): InProgress[] {
	const out: InProgress[] = [];
	for (const runner of r) {
		const target = runner.current;
		if (!target || runner.state === 'needs_login') continue;
		const req = target.required_minutes;
		const pct = req > 0 ? Math.min(100, Math.round((target.minutes / req) * 100)) : 0;
		out.push({ target, runner, pct, left: Math.max(0, req - target.minutes) });
	}
	return out.sort((a, b) => b.pct - a.pct);
}

export function pageQuery(q: ClaimsQuery, page: number, size: number): ClaimsQuery {
	return { ...q, limit: size, offset: (Math.max(1, page) - 1) * size };
}

export function pageCount(total: number, size: number): number {
	return Math.max(1, Math.ceil(total / size));
}

/** 1-based item range shown on a page; 0 to 0 when there is nothing. */
export function pageRange(page: number, size: number, total: number): { from: number; to: number } {
	if (total <= 0) return { from: 0, to: 0 };
	const from = (page - 1) * size + 1;
	return { from, to: Math.min(total, page * size) };
}

export function validPageSize(n: number): number {
	return (PAGE_SIZES as readonly number[]).includes(n) ? n : DEFAULT_PAGE_SIZE;
}

export function readPageSize(): number {
	try {
		return validPageSize(Number(localStorage.getItem(PAGE_SIZE_KEY)));
	} catch {
		return DEFAULT_PAGE_SIZE;
	}
}

export function writePageSize(n: number): void {
	try {
		localStorage.setItem(PAGE_SIZE_KEY, String(n));
	} catch {
		/* storage unavailable: the choice lasts for this visit */
	}
}
