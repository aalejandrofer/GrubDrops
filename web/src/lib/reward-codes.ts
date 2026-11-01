import type { RewardCode } from '$lib/api/types';

const HOUR = 3600_000;
const DAY = 24 * HOUR;

/** Time left before a code expires: whole days from one day up, else whole hours (at least 1). Null when no expiry is known or it passed. */
export function expiresIn(iso: string | null, now: number): { key: 'rewards.expires_days' | 'rewards.expires_hours'; n: number } | null {
	if (iso === null) return null;
	const at = Date.parse(iso);
	if (!Number.isFinite(at) || at <= now) return null;
	const left = at - now;
	if (left >= DAY) return { key: 'rewards.expires_days', n: Math.floor(left / DAY) };
	return { key: 'rewards.expires_hours', n: Math.max(1, Math.floor(left / HOUR)) };
}

/** Codes still worth showing: not expired at now. */
export function liveCodes(codes: RewardCode[], now: number): RewardCode[] {
	return codes.filter((c) => c.expires_at === null || Date.parse(c.expires_at) > now);
}

/** True for an absolute http(s) link, safe to put in href. */
export function redeemHref(url: string): string | null {
	return /^https?:\/\/[^\s]+$/i.test(url) ? url : null;
}
