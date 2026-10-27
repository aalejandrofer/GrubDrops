import { accountStatus } from "$lib/accounts";
import type {
  Account,
  Claim,
  Game,
  Overview,
  Platform,
  Runner,
  Target,
} from "$lib/api/types";
import { dayBucket } from "$lib/format";

export function normName(s: string): string {
  return s.trim().toLowerCase().replace(/\s+/g, " ");
}

export type AccountRow = {
  runner: Runner;
  progress: number;
};

export type GameCard = {
  game: Game;
  rank: number;
  /** 1-based position on Home; sets the card layout. */
  pos: number;
  size: "lg" | "sm";
  rows: AccountRow[];
  queued: Target[];
  /** What the game's queued drops wait for; null when none is queued. */
  waitFor: WaitFor | null;
  moreInUpNext: number;
};

/**
 * live: an idle account has a drop queued, so it waits for an eligible
 * channel to go live; channels is empty when any channel qualifies, more
 * counts the channels not named. account: every account with a drop queued
 * is mining something else.
 */
export type WaitFor =
  | { kind: "live"; channels: string[]; more: number }
  | { kind: "account"; account: string };

function row(r: Runner): AccountRow {
  const c = r.current;
  const progress =
    c && c.required_minutes > 0 ? c.minutes / c.required_minutes : 0;
  return { runner: r, progress };
}

/** A runner on a card: it has a current drop and is neither paused nor signed out. */
function onCard(r: Runner, paused: ReadonlySet<string>): boolean {
  return (
    r.current !== null && r.state !== "needs_login" && !paused.has(r.account_id)
  );
}

/**
 * Cards in priority order. A card lists only the accounts mining its game;
 * notMining lists the rest. A runner game matches the card by name or slug,
 * like the Go planner.
 */
export function gameCards(
  g: Game[],
  r: Runner[],
  paused: ReadonlySet<string> = new Set(),
): GameCard[] {
  const sorted = g.slice().sort((a, b) => a.priority - b.priority);
  return sorted.map((game, i) => {
    const rank = i + 1;
    const keys = new Set(
      [normName(game.name), normName(game.slug ?? "")].filter(Boolean),
    );
    const isThis = (name: string) => keys.has(normName(name));
    const rows = r
      .filter((x) => onCard(x, paused) && isThis(x.current!.game))
      .map(row)
      .sort((a, b) => b.progress - a.progress);
    const targets: { t: Target; by: string; idle: boolean; out: boolean }[] =
      [];
    for (const x of r) {
      if (paused.has(x.account_id)) continue;
      for (const t of x.up_next)
        if (isThis(t.game))
          targets.push({
            t,
            by: x.account_name,
            idle: !x.current,
            out: x.state === "needs_login",
          });
    }
    const seen = new Set<string>();
    const queued = targets
      .filter((x) => !seen.has(x.t.benefit_id) && seen.add(x.t.benefit_id))
      .map((x) => x.t);
    return {
      game,
      rank,
      pos: rank,
      size: rank <= 2 ? "lg" : "sm",
      rows,
      queued,
      waitFor: waitFor(targets.filter((x) => !x.out)),
      moreInUpNext: queued.length,
    };
  });
}

function waitFor(
  targets: { t: Target; by: string; idle: boolean }[],
): WaitFor | null {
  const idle = targets.find((x) => x.idle);
  if (idle) {
    const channels = idle.t.channels ?? [];
    const more = Math.max(0, (idle.t.channel_count ?? 0) - channels.length);
    return { kind: "live", channels, more };
  }
  return targets.length > 0 ? { kind: "account", account: targets[0].by } : null;
}

/**
 * At most max cards for Home. Mining cards come first, then waiting or queued
 * ones, then the rest, each group in rank order, so a game that is mining is
 * never listed under one that only waits. Layout follows the display
 * position: the first two shown are large.
 */
export function visibleCards(
  cards: GameCard[],
  max = 7,
): { shown: GameCard[]; hidden: number } {
  const tier = (c: GameCard) =>
    c.game.state === "mining" || c.rows.length > 0
      ? 0
      : c.game.state === "waiting" || c.queued.length > 0
        ? 1
        : 2;
  const ordered = [0, 1, 2].flatMap((n) => cards.filter((c) => tier(c) === n));
  const shown = ordered
    .slice(0, max)
    .map((c, i) => ({
      ...c,
      pos: i + 1,
      size: i < 2 ? ("lg" as const) : ("sm" as const),
    }));
  return { shown, hidden: cards.length - shown.length };
}

/**
 * How many cards fit in `available` px when stacked with `gap` between them.
 * Never fewer than `min` (or the card count, if lower).
 */
export function fitCount(
  cardHeights: number[],
  gap: number,
  available: number,
  min: number,
): number {
  let used = 0;
  let n = 0;
  for (const h of cardHeights) {
    const next = used + h + (n > 0 ? gap : 0);
    if (next > available) break;
    used = next;
    n++;
  }
  return Math.min(cardHeights.length, Math.max(n, min));
}

export function foldRows(
  rows: AccountRow[],
  limit = 3,
): { shown: AccountRow[]; hidden: number } {
  return {
    shown: rows.slice(0, limit),
    hidden: Math.max(0, rows.length - limit),
  };
}

export type IdleState = "idle" | "signed_out" | "paused";

export type IdleChip = {
  id: string;
  name: string;
  platform: Platform;
  state: IdleState;
};

const IDLE_ORDER: IdleState[] = ["idle", "signed_out", "paused"];

/**
 * Accounts no card lists, for the chips under the last card: idle, then
 * signed out, then paused, each in account order. Force watching accounts are
 * left out; the force watch tile names them.
 */
export function notMining(accounts: Account[], runners: Runner[]): IdleChip[] {
  const paused = new Set(accounts.filter((a) => !a.enabled).map((a) => a.id));
  const chips: IdleChip[] = [];
  for (const a of accounts) {
    const r = runners.find((x) => x.account_id === a.id);
    if (r && onCard(r, paused)) continue;
    const s = accountStatus(a, r);
    if (s === "mining" || s === "force_watching") continue;
    chips.push({ id: a.id, name: a.name, platform: a.platform, state: s });
  }
  return IDLE_ORDER.flatMap((s) => chips.filter((c) => c.state === s));
}

/** The campaign a card names: the one an account mines, else the game's first. */
export function cardCampaign(
  card: GameCard,
): { platform: Platform; id: string; name: string } | null {
  const cur = card.rows.find((r) => r.runner.current)?.runner.current;
  if (cur)
    return {
      platform: cur.platform,
      id: cur.campaign_id,
      name: cur.campaign_name,
    };
  const c = card.game.campaigns[0];
  return c ? { platform: c.platform, id: c.id, name: c.name } : null;
}

/**
 * What Home shows. Every view has a next step: a retry, a link, or a line
 * that says when mining starts.
 */
export type HomeView =
  | "loading"
  | "error"
  | "offline"
  | "first_run"
  | "no_games"
  | "paused"
  | "signed_out"
  | "nothing_live"
  | "healthy";

export type HomeInput = {
  overview: Overview | null;
  /** Chosen games, or null before they load. */
  games: Game[] | null;
  overviewFailed: boolean;
  gamesFailed: boolean;
  unreachable: boolean;
  /** Some account is force watching now. */
  forceWatching?: boolean;
};

export function homeView(i: HomeInput): HomeView {
  const o = i.overview;
  if (!o)
    return i.unreachable ? "offline" : i.overviewFailed ? "error" : "loading";
  if (o.status.state === "no_accounts") return "first_run";
  // Force watch channels keep accounts busy without games, so Home shows the
  // dashboard with the force watch tile instead of the empty state. The
  // server still says no_games when no channel has an enabled account.
  const forced =
    (o.force_watch != null && o.status.state !== "no_games") ||
    i.forceWatching === true;
  if (!forced && (o.status.state === "no_games" || i.games?.length === 0))
    return "no_games";
  // Signed out wins over paused, as on the Accounts list.
  const signedOut = new Set(
    o.problems.filter((p) => p.kind === "needs_login").map((p) => p.account_id),
  );
  if (signedOut.size >= o.status.accounts_total) return "signed_out";
  if ((o.status.accounts_paused ?? 0) >= o.status.accounts_total)
    return "paused";
  if (!i.games) return i.gamesFailed && !i.unreachable ? "error" : "loading";
  if (
    o.status.state === "nothing_live" ||
    i.games.every((g) => g.state === "no_campaign")
  )
    return "nothing_live";
  return "healthy";
}

export function groupClaims(
  c: Claim[],
  now: number,
  zone: string,
): { today: Claim[]; yesterday: Claim[]; earlier: Claim[] } {
  const out: { today: Claim[]; yesterday: Claim[]; earlier: Claim[] } = {
    today: [],
    yesterday: [],
    earlier: [],
  };
  for (const x of c) out[dayBucket(x.claimed_at, now, zone)].push(x);
  return out;
}
