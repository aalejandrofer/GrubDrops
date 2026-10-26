import type { ClaimsQuery, Runner, Target } from '$lib/api/types';

export const PAGE = 10;

export type InProgress = { target: Target; runner: Runner; pct: number; left: number };

/** Drops being mined now, furthest along first. */
export function inProgress(r: Runner[]): InProgress[] {
	const out: InProgress[] = [];
	for (const runner of r) {
		const target = runner.current;
		if (!target) continue;
		const req = target.required_minutes;
		const pct = req > 0 ? Math.min(100, Math.round((target.minutes / req) * 100)) : 0;
		out.push({ target, runner, pct, left: Math.max(0, req - target.minutes) });
	}
	return out.sort((a, b) => b.pct - a.pct);
}

export function queuedCount(r: Runner[]): number {
	return r.reduce((n, x) => n + x.up_next.length, 0);
}

export function nextQuery(q: ClaimsQuery, loaded: number): ClaimsQuery {
	return { ...q, limit: PAGE, offset: loaded };
}
