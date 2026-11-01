import type { Account, ForceChannel, Platform, PointsBalance, PointsResp, Runner } from '$lib/api/types';
import { accountLine, accountStatus, forceWatchOn, pointsSupported } from '$lib/accounts';
import { parseChannel, watchingIds } from '$lib/force-watch';
import { relTime, uptime } from '$lib/format';
import { t, tn } from '$lib/i18n/index.svelte';
import { moveItem } from '$lib/upnext';

export const PLATFORMS: Platform[] = ['twitch', 'kick'];

/** Stable key of a Force watch channel. Channel names hold only [a-z0-9_-]. */
export const chanKey = (c: Pick<ForceChannel, 'platform' | 'channel'>): string => `${c.platform}/${c.channel}`;

/** One platform's channels, in global rank order. */
export function tabRows(list: readonly ForceChannel[], platform: Platform): ForceChannel[] {
	return list.filter((c) => c.platform === platform);
}

/**
 * Moves the tab row at from to to. The list is global, so the other
 * platform's channels keep their slots and the tab's channels refill theirs.
 */
export function moveInTab(list: readonly ForceChannel[], platform: Platform, from: number, to: number): ForceChannel[] {
	const rows = moveItem(tabRows(list, platform), from, to);
	let i = 0;
	return list.map((c) => (c.platform === platform ? rows[i++] : c));
}

/** Ticks or unticks an account on one channel. */
export function toggleAccount(list: readonly ForceChannel[], key: string, id: string): ForceChannel[] {
	return list.map((c) =>
		chanKey(c) !== key
			? c
			: { ...c, accounts: c.accounts.includes(id) ? c.accounts.filter((x) => x !== id) : [...c.accounts, id] }
	);
}

export function removeChannel(list: readonly ForceChannel[], key: string): ForceChannel[] {
	return list.filter((c) => chanKey(c) !== key);
}

export type AddResult = { list: ForceChannel[]; key: string } | { err: 'bad' | 'dupe' | 'other_tab'; platform?: Platform };

/** Adds a pasted channel link at the bottom with no accounts ticked. It must belong to the open tab. */
export function addChannel(list: readonly ForceChannel[], raw: string, tab: Platform): AddResult {
	const p = parseChannel(raw);
	if (!p) return { err: 'bad' };
	if (p.platform !== tab) return { err: 'other_tab', platform: p.platform };
	if (list.some((c) => c.platform === p.platform && c.channel === p.name)) return { err: 'dupe' };
	const c: ForceChannel = { platform: p.platform, channel: p.name, live: null, accounts: [], watching: [] };
	return { list: [...list, c], key: chanKey(c) };
}

const balKey = (platform: Platform, channel: string, id: string) => `${platform}/${channel}/${id}`;

/** Balances from GET /api/points, looked up by channel and account. */
export function balanceIndex(p: PointsResp | null): Map<string, PointsBalance> {
	const out = new Map<string, PointsBalance>();
	for (const c of p?.channels ?? []) for (const b of c.balances) out.set(balKey(c.platform, c.channel, b.account_id), b);
	return out;
}

export function balanceOf(
	idx: ReadonlyMap<string, PointsBalance>,
	c: Pick<ForceChannel, 'platform' | 'channel'>,
	id: string
): PointsBalance | undefined {
	return idx.get(balKey(c.platform, c.channel, id));
}

export type BalTone = 'ok' | 'stale' | 'error' | 'pending' | 'off' | 'na';

/** Tooltip for each tone. ok, stale, error and off take {time}. */
export const BAL_KEYS: Record<BalTone, string> = {
	ok: 'points.bal_ok',
	stale: 'points.bal_stale',
	error: 'points.bal_error',
	pending: 'points.bal_pending',
	off: 'points.bal_off',
	na: 'points.bal_na'
};

export type BalView = { value: number | null; tone: BalTone; at: string | null };

/** How a chip shows one balance. An error or off balance is the last good one. */
export function balanceView(b: PointsBalance | undefined): BalView | null {
	if (!b) return null;
	const at = b.fetched_at;
	switch (b.status) {
		case 'ok':
			return { value: b.balance, tone: b.stale ? 'stale' : 'ok', at };
		case 'error':
			return { value: b.balance, tone: 'error', at };
		case 'off':
			return { value: b.balance, tone: 'off', at };
		case 'pending':
			return { value: null, tone: 'pending', at: null };
		default:
			return { value: null, tone: 'na', at: null };
	}
}

export type Fresh = { kind: 'none' } | { kind: 'pending' } | { kind: 'ok'; at: string; stale: boolean } | { kind: 'error'; at: string | null };

const newest = (xs: string[]) => xs.reduce((a, b) => (Date.parse(b) > Date.parse(a) ? b : a));
const oldest = (xs: string[]) => xs.reduce((a, b) => (Date.parse(b) < Date.parse(a) ? b : a));

/**
 * How fresh an account's balances are. A failed read wins and names the age
 * of the oldest value it falls back to; else the newest read, stale when any
 * value is.
 */
export function accountFresh(p: PointsResp | null, id: string): Fresh {
	const bs = (p?.channels ?? []).flatMap((c) => c.balances).filter((b) => b.account_id === id && b.status !== 'unavailable');
	const errs = bs.filter((b) => b.status === 'error');
	if (errs.length) {
		const at = errs.map((b) => b.fetched_at).filter((x): x is string => x !== null);
		return { kind: 'error', at: at.length ? oldest(at) : null };
	}
	const read = bs.filter((b) => b.fetched_at !== null);
	if (read.length) {
		const at = newest(read.map((b) => b.checked_at ?? (b.fetched_at as string)));
		return { kind: 'ok', at, stale: read.some((b) => b.stale) };
	}
	return bs.some((b) => b.status === 'pending') ? { kind: 'pending' } : { kind: 'none' };
}

/** "Updated 5 min ago" and the like for an account's balances; '' when there is nothing to say. */
export function freshLine(f: Fresh, now: number): string {
	if (f.kind === 'ok') return t('points.fresh_ok', { time: relTime(f.at, now) });
	if (f.kind === 'error') return f.at ? t('points.fresh_error', { time: relTime(f.at, now) }) : t('points.fresh_error_none');
	if (f.kind === 'pending') return t('points.fresh_pending');
	return '';
}

/**
 * Delay to the Points page's next clock tick: 5s while any balance time is
 * under a minute old, so "0s ago" keeps up, and 30s once all are older.
 */
export function pointsTickMs(p: PointsResp | null, now: number): number {
	const fresh = (p?.channels ?? []).some((c) =>
		c.balances.some((b) => [b.checked_at, b.fetched_at].some((at) => at !== null && now - Date.parse(at) < 60_000))
	);
	return fresh ? 5_000 : 30_000;
}

/** Tone class for freshLine: failed, stale or plain. */
export const freshTone = (f: Fresh): string => (f.kind === 'error' ? 'failed' : f.kind === 'ok' && f.stale ? 'stale' : '');

/** i18n key: why Force watch runs for the account or not, right now. */
export function fwNote(a: Account, r: Runner | undefined): string {
	if (!a.enabled) return 'points.fw_paused';
	if (!forceWatchOn(a)) return 'points.fw_points_off';
	if (pointsSupported(a) && a.points_mode === 'points_only') return 'points.fw_always';
	if (accountStatus(a, r) === 'mining') return 'points.fw_waits';
	return 'points.fw_on';
}

/** Who force watches which live channel, in rank order. */
export function watchers(
	list: readonly ForceChannel[],
	runners: readonly Runner[],
	names: ReadonlyMap<string, string>
): { name: string; channel: string }[] {
	return list.flatMap((c) =>
		c.live ? watchingIds(c, runners).map((id) => ({ name: names.get(id) ?? id, channel: c.channel })) : []
	);
}

/**
 * Header status: "Points watching · seed on shroud, alt on xqc" for one or
 * two watchers, "Points watching · 3 accounts" past that. undefined with none.
 */
export function watchStatus(who: readonly { name: string; channel: string }[]): string | undefined {
	if (who.length === 0) return undefined;
	if (who.length > 2) return tn('points.watching_n', who.length);
	return t('points.watching', { who: who.map((w) => t('points.on_channel', w)).join(', ') });
}

/** An account's points total for its card; null on platforms without balances (Kick) or with none known. */
export function pointsTotal(p: PointsResp | null, id: string): number | null {
	const a = p?.accounts.find((x) => x.id === id);
	return a && a.balances_available ? a.total : null;
}

export type StatePart = { text: string; kind: 'text' | 'channel' | 'time' };
export type CardState = { on: boolean; text: string; parts: StatePart[] };

/**
 * An account card's state line. While force watching it reads "Watching
 * shroud · 37m", split so the channel and time can be styled. started_at
 * outlives force watching, so every other state keeps the account line.
 */
export function cardState(a: Account, r: Runner | undefined, now: number): CardState {
	const time = r?.started_at ? uptime(r.started_at, now) : '';
	if (r && accountStatus(a, r) === 'force_watching' && r.channel && time) {
		const vars = { channel: r.channel, time };
		const parts = t('points.card_watching')
			.split(/(\{channel\}|\{time\})/)
			.filter(Boolean)
			.map((x): StatePart => {
				if (x === '{channel}') return { text: r.channel, kind: 'channel' };
				if (x === '{time}') return { text: time, kind: 'time' };
				return { text: x, kind: 'text' };
			});
		return { on: true, text: t('points.card_watching', vars), parts };
	}
	const text = accountLine(a, r);
	return { on: false, text, parts: [{ text, kind: 'text' }] };
}
