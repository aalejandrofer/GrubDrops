import type { Claim, Game, Overview, Runner, Target } from '$lib/api/types';
import { dayBucket } from '$lib/format';

export function normName(s: string): string {
	return s.trim().toLowerCase().replace(/\s+/g, ' ');
}

export type AccountRow = { runner: Runner; progress: number; muted: boolean };

export type GameCard = {
	game: Game;
	rank: number;
	size: 'lg' | 'sm';
	rows: AccountRow[];
	queued: Target[];
	/** Name of the first account with this game queued. */
	queuedBy: string | null;
	moreInUpNext: number;
};

function row(r: Runner): AccountRow {
	const c = r.current;
	const progress = c && c.required_minutes > 0 ? c.minutes / c.required_minutes : 0;
	return { runner: r, progress, muted: r.state === 'needs_login' };
}

/**
 * Cards in priority order. Rank 1 also lists signed-out and idle accounts,
 * since they have no current game to match.
 */
export function gameCards(g: Game[], r: Runner[]): GameCard[] {
	const sorted = g.slice().sort((a, b) => a.priority - b.priority);
	return sorted.map((game, i) => {
		const rank = i + 1;
		const key = normName(game.name);
		const rows = r
			.filter((x) => (x.state === 'needs_login' || !x.current ? rank === 1 : normName(x.current.game) === key))
			.map(row)
			.sort((a, b) => b.progress - a.progress);
		const targets: { t: Target; by: string }[] = [];
		for (const x of r) for (const t of x.up_next) if (normName(t.game) === key) targets.push({ t, by: x.account_name });
		const seen = new Set<string>();
		const queued = targets.filter((x) => !seen.has(x.t.benefit_id) && seen.add(x.t.benefit_id)).map((x) => x.t);
		return {
			game,
			rank,
			size: rank <= 2 ? 'lg' : 'sm',
			rows,
			queued,
			queuedBy: targets[0]?.by ?? null,
			moreInUpNext: queued.length
		};
	});
}

export function foldRows(
	rows: AccountRow[],
	limit = 3
): { shown: AccountRow[]; hidden: number; mining: number; idle: number } {
	const mining = rows.filter((x) => !x.muted && x.runner.current !== null).length;
	const idle = rows.filter((x) => !x.muted && x.runner.current === null).length;
	return { shown: rows.slice(0, limit), hidden: Math.max(0, rows.length - limit), mining, idle };
}

export type HomeState = 'first_run' | 'healthy' | 'nothing_live' | 'offline';

export function homeState(o: Overview | null, unreachable: boolean): HomeState {
	if (unreachable) return 'offline';
	switch (o?.status.state) {
		case 'no_accounts':
		case 'no_games':
			return 'first_run';
		case 'nothing_live':
			return 'nothing_live';
		default:
			return 'healthy';
	}
}

export function groupClaims(
	c: Claim[],
	now: number,
	zone: string
): { today: Claim[]; yesterday: Claim[]; earlier: Claim[] } {
	const out: { today: Claim[]; yesterday: Claim[]; earlier: Claim[] } = { today: [], yesterday: [], earlier: [] };
	for (const x of c) out[dayBucket(x.claimed_at, now, zone)].push(x);
	return out;
}
