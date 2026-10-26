import { i18n, t } from '$lib/i18n/index.svelte';

const SPACES = /[\u202f\u00a0]/g;

export function relTime(iso: string | null, now: number): string {
	if (iso === null) return t('time.never');
	const at = Date.parse(iso);
	if (!Number.isFinite(at)) return t('time.never');
	const s = Math.max(0, Math.floor((now - at) / 1000));
	if (s < 60) return t('time.s_ago', { n: s });
	if (s < 3600) return t('time.min_ago', { n: Math.floor(s / 60) });
	if (s < 48 * 3600) return t('time.h_ago', { n: Math.floor(s / 3600) });
	return t('time.d_ago', { n: Math.floor(s / 86400) });
}

export function formatInt(n: number): string {
	return new Intl.NumberFormat(i18n.lang).format(n);
}

export function etaShort(min: number): string {
	const m = Math.max(0, Math.round(min));
	return m >= 60 ? `${Math.floor(m / 60)}h ${m % 60}m` : `${m}m`;
}

export function uptime(startedIso: string, now: number): string {
	const at = Date.parse(startedIso);
	if (!Number.isFinite(at)) return '';
	const m = Math.max(0, Math.floor((now - at) / 60000));
	const d = Math.floor(m / 1440);
	const h = Math.floor((m % 1440) / 60);
	if (d > 0) return `${d}d ${h}h`;
	if (h > 0) return `${h}h ${m % 60}m`;
	return `${m}m`;
}

export function clock(iso: string, zone: string, fmt: '24h' | '12h'): string {
	return new Intl.DateTimeFormat('en-US', {
		timeZone: zone,
		hour: fmt === '24h' ? '2-digit' : 'numeric',
		minute: '2-digit',
		hourCycle: fmt === '24h' ? 'h23' : 'h12'
	})
		.format(new Date(iso))
		.replace(SPACES, ' ');
}

function dayKey(ms: number, zone: string): string {
	return new Intl.DateTimeFormat('en-CA', {
		timeZone: zone,
		year: 'numeric',
		month: '2-digit',
		day: '2-digit'
	}).format(new Date(ms));
}

/** Calendar day before a YYYY-MM-DD string, by UTC date math. */
function prevDay(ymd: string): string {
	const d = new Date(`${ymd}T00:00:00Z`);
	d.setUTCDate(d.getUTCDate() - 1);
	return d.toISOString().slice(0, 10);
}

export function dayBucket(iso: string, now: number, zone: string): 'today' | 'yesterday' | 'earlier' {
	const k = dayKey(Date.parse(iso), zone);
	if (k === dayKey(now, zone)) return 'today';
	if (k === prevDay(dayKey(now, zone))) return 'yesterday';
	return 'earlier';
}

export function shortDate(iso: string, zone: string): string {
	return new Intl.DateTimeFormat(i18n.lang, { timeZone: zone, month: 'short', day: 'numeric' })
		.format(new Date(iso))
		.replace(SPACES, ' ');
}

export function claimStamp(iso: string, now: number, zone: string, fmt: '24h' | '12h'): string {
	const b = dayBucket(iso, now, zone);
	if (b === 'today') return t('time.today_at', { time: clock(iso, zone, fmt) });
	if (b === 'yesterday') return t('time.yesterday_at', { time: clock(iso, zone, fmt) });
	return shortDate(iso, zone);
}

export function nextScanMin(lastScanIso: string | null, now: number, everySec = 300): number {
	if (lastScanIso === null) return 0;
	const left = Date.parse(lastScanIso) + everySec * 1000 - now;
	return Math.max(0, Math.ceil(left / 60000));
}

/** The URL when it is https, else ''. Links from outside data render only then. */
export function httpsOnly(url: string | null | undefined): string {
	return url?.startsWith('https://') ? url : '';
}
