import { beforeEach, expect, test } from 'vitest';
import {
	httpsOnly,
	claimStamp,
	clock,
	dayBucket,
	etaShort,
	formatInt,
	nextScanMin,
	relTime,
	shortDate,
	uptime
} from './format';
import { setLang } from './i18n/index.svelte';

const now = Date.parse('2026-10-25T10:00:00Z');
const ago = (s: number) => new Date(now - s * 1000).toISOString();

beforeEach(() => setLang('en'));

test('relTime', () => {
	expect(relTime(ago(41), now)).toBe('41 s ago');
	expect(relTime(ago(180), now)).toBe('3 min ago');
	expect(relTime(ago(2 * 3600), now)).toBe('2 h ago');
	expect(relTime(ago(4 * 86400), now)).toBe('4 d ago');
	expect(relTime(null, now)).toBe('never');
	expect(relTime(ago(-30), now)).toBe('0 s ago');
});

test('formatInt follows the language', () => {
	expect(formatInt(18240)).toBe('18,240');
	setLang('es');
	expect(formatInt(18240)).toBe('18.240');
});

test('etaShort', () => {
	expect(etaShort(18)).toBe('18m');
	expect(etaShort(75)).toBe('1h 15m');
	expect(etaShort(0)).toBe('0m');
	expect(etaShort(718)).toBe('11h 58m');
});

test('uptime', () => {
	expect(uptime(ago(6 * 86400 + 4 * 3600), now)).toBe('6d 4h');
	expect(uptime(ago(3 * 3600 + 12 * 60), now)).toBe('3h 12m');
	expect(uptime(ago(4 * 60), now)).toBe('4m');
});

test('clock', () => {
	expect(clock('2026-10-25T09:12:00Z', 'UTC', '24h')).toBe('09:12');
	expect(clock('2026-10-25T09:12:00Z', 'UTC', '12h')).toBe('9:12 AM');
});

test('dayBucket uses the settings zone', () => {
	const iso = '2026-10-24T23:30:00Z';
	expect(dayBucket(iso, now, 'Asia/Shanghai')).toBe('today');
	expect(dayBucket(iso, now, 'UTC')).toBe('yesterday');
	expect(dayBucket('2026-10-23T10:00:00Z', now, 'UTC')).toBe('earlier');
});

test('shortDate', () => {
	expect(shortDate('2026-10-23T10:00:00Z', 'UTC')).toBe('Oct 23');
});

test('claimStamp', () => {
	expect(claimStamp('2026-10-25T09:12:00Z', now, 'UTC', '24h')).toBe('Today 09:12');
	expect(claimStamp('2026-10-24T22:05:00Z', now, 'UTC', '24h')).toBe('Yest. 22:05');
	expect(claimStamp('2026-10-23T10:00:00Z', now, 'UTC', '24h')).toBe('Oct 23');
});

test('nextScanMin', () => {
	expect(nextScanMin(ago(120), now)).toBe(3);
	expect(nextScanMin(ago(400), now)).toBe(0);
	expect(nextScanMin(null, now)).toBe(0);
});

test('dayBucket yesterday across a DST spring-forward', () => {
	const zone = 'America/New_York';
	const n = Date.parse('2026-03-09T12:00:00Z');
	expect(dayBucket('2026-03-08T12:00:00Z', n, zone)).toBe('yesterday');
	expect(dayBucket('2026-03-08T05:30:00Z', n, zone)).toBe('yesterday');
	expect(dayBucket('2026-03-07T12:00:00Z', n, zone)).toBe('earlier');
	// 00:30 EDT on 03-09: a fixed now-24h lands on 03-07 and would miss 01:00 EST on 03-08.
	const early = Date.parse('2026-03-09T04:30:00Z');
	expect(dayBucket('2026-03-08T06:00:00Z', early, zone)).toBe('yesterday');
});

test('unparseable iso is handled', () => {
	expect(relTime('garbage', now)).toBe('never');
	expect(uptime('garbage', now)).toBe('');
});

test('httpsOnly keeps https links only', () => {
	expect(httpsOnly('https://github.com/x/releases/tag/v2')).toBe('https://github.com/x/releases/tag/v2');
	expect(httpsOnly('javascript:alert(1)')).toBe('');
	expect(httpsOnly('http://x')).toBe('');
	expect(httpsOnly(undefined)).toBe('');
});
