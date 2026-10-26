import { etaShort, relTime } from '$lib/format';

export type StampPart = { key: string; vars: Record<string, string> };

/** Scan line parts, each a locale key with its vars. Empty next scan keeps the plain last-scan text. */
export function scanStamp(last: string | null, next: string | null, now: Date): StampPart[] {
	const nowMs = now.getTime();
	const nextAt = next === null ? NaN : Date.parse(next);
	const time = () => etaShort(Math.max(1, Math.ceil((nextAt - nowMs) / 60000)));
	if (last === null && Number.isFinite(nextAt)) return [{ key: 'scan.first_in', vars: { time: time() } }];
	const parts: StampPart[] = [{ key: 'home.last_scan', vars: { rel: relTime(last, nowMs) } }];
	if (last !== null && Number.isFinite(nextAt)) parts.push({ key: 'scan.next_in', vars: { time: time() } });
	return parts;
}
