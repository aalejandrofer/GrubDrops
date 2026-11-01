import type { Account, PointsResp, Runner, Target } from '$lib/api/types';
import type { AccountStatus } from '$lib/accounts';
import { canMineNow } from '$lib/upnext';

/** i18n key of each status pill. */
export const STATUS_KEYS: Record<AccountStatus, string> = {
	mining: 'accounts.status.mining',
	force_watching: 'accounts.status.force_watching',
	sleep_window: 'accounts.status.sleep_window',
	signed_out: 'accounts.status.signed_out',
	idle: 'accounts.status.idle',
	paused: 'accounts.status.paused'
};

/** Quick actions in a row's ⋯ menu, in order. Remove and the other settings live in the detail drawer. */
export type MenuAction = 'details' | 'check' | 'pause' | 'resume' | 'restart' | 'skip_sleep';

export function menuActions(a: Pick<Account, 'enabled'>): MenuAction[] {
	return a.enabled ? ['details', 'check', 'pause', 'restart', 'skip_sleep'] : ['details', 'check', 'resume', 'skip_sleep'];
}

/** The drop Mine now would switch to: the first Up next item. */
export function nextDrop(r: Runner | undefined): Target | null {
	return r?.up_next[0] ?? null;
}

/**
 * i18n key for why Mine now can't run for the account, or null when it can.
 * Signed out wins, then paused, then an empty queue, then the runner state.
 */
export function mineBlock(a: Pick<Account, 'enabled' | 'needs_login'>, r: Runner | undefined): string | null {
	if (a.needs_login || r?.state === 'needs_login') return 'accounts.login_first';
	if (!a.enabled) return 'accounts.resume_first';
	if (!nextDrop(r)) return 'accounts.next_none';
	if (r?.state === 'sleep_window') return 'upnext.mine_sleep_window';
	if (r && !canMineNow(r)) return 'accounts.mine_idle';
	return null;
}

/** The account's channel-points total from GET /api/points; null when none is known. */
export function pointsTotal(p: PointsResp | null, id: string): number | null {
	return p?.accounts.find((x) => x.id === id)?.total ?? null;
}

/** Key for one saving field of one account, for the saving, saved and failed marks. */
export type Field = 'name' | 'enabled' | 'own' | 'skip' | 'points';

export const fieldKey = (id: string, f: Field): string => `${id}:${f}`;

/** Result lines for actions that aren't plain saves. */
export type NoteKind = 'login' | 'run' | 'mine' | 'remove';

/** Which result line a collapsed row shows, newest kind first: login check, restart, Mine now, then failed saves. */
export const ROW_NOTES: string[] = ['login', 'run', 'mine', 'enabled', 'skip'];
