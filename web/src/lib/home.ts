import type { Claim, Game, Overview, Runner, Target } from '$lib/api/types';
import { dayBucket } from '$lib/format';

export function normName(s: string): string {
	return s.trim().toLowerCase().replace(/\s+/g, ' ');
}

/** muted: signed out or paused, so not mining. */
export type AccountRow = { runner: Runner; progress: number; muted: boolean; paused: boolean };

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

function row(r: Runner, paused: boolean): AccountRow {
	const c = r.current;
	const progress = c && c.required_minutes > 0 ? c.minutes / c.required_minutes : 0;
	return { runner: r, progress, muted: paused || r.state === 'needs_login', paused };
}

/**
 * Cards in priority order. Rank 1 also lists signed-out, paused and idle
 * accounts, since they have no current game to match. A runner game matches
 * the card by name or slug, like the Go planner.
 */
export function gameCards(g: Game[], r: Runner[], paused: ReadonlySet<string> = new Set()): GameCard[] {
	const sorted = g.slice().sort((a, b) => a.priority - b.priority);
	return sorted.map((game, i) => {
		const rank = i + 1;
		const keys = new Set([normName(game.name), normName(game.slug ?? '')].filter(Boolean));
		const isThis = (name: string) => keys.has(normName(name));
		const off = (x: Runner) => paused.has(x.account_id) || x.state === 'needs_login' || !x.current;
		const rows = r
			.filter((x) => (off(x) ? rank === 1 : isThis(x.current!.game)))
			.map((x) => row(x, paused.has(x.account_id)))
			.sort((a, b) => b.progress - a.progress);
		const targets: { t: Target; by: string }[] = [];
		for (const x of r) {
			if (paused.has(x.account_id)) continue;
			for (const t of x.up_next) if (isThis(t.game)) targets.push({ t, by: x.account_name });
		}
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

/**
 * What Home shows. Every view has a next step: a retry, a link, or a line
 * that says when mining starts.
 */
export type HomeView =
	| 'loading'
	| 'error'
	| 'offline'
	| 'first_run'
	| 'no_games'
	| 'paused'
	| 'signed_out'
	| 'nothing_live'
	| 'healthy';

export type HomeInput = {
	overview: Overview | null;
	/** Chosen games, or null before they load. */
	games: Game[] | null;
	overviewFailed: boolean;
	gamesFailed: boolean;
	unreachable: boolean;
};

export function homeView(i: HomeInput): HomeView {
	const o = i.overview;
	if (!o) return i.unreachable ? 'offline' : i.overviewFailed ? 'error' : 'loading';
	if (o.status.state === 'no_accounts') return 'first_run';
	if (o.status.state === 'no_games' || i.games?.length === 0) return 'no_games';
	// Paused wins over signed out, as on the Accounts list.
	if ((o.status.accounts_paused ?? 0) >= o.status.accounts_total) return 'paused';
	if (!i.games) return i.gamesFailed && !i.unreachable ? 'error' : 'loading';
	const signedOut = new Set(o.problems.filter((p) => p.kind === 'needs_login').map((p) => p.account_id));
	if (signedOut.size >= o.status.accounts_total) return 'signed_out';
	if (o.status.state === 'nothing_live' || i.games.every((g) => g.state === 'no_campaign')) return 'nothing_live';
	return 'healthy';
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
