import { describe, expect, it } from 'vitest';
import { scanStamp } from './scan';

const now = new Date('2026-10-25T12:00:00Z');
const at = (s: number) => new Date(now.getTime() + s * 1000).toISOString();

describe('scanStamp', () => {
	it('no last scan and a next one: first scan in', () => {
		expect(scanStamp(null, at(180), now)).toEqual([{ key: 'scan.first_in', vars: { time: '3m' } }]);
	});
	it('last and next: last scan plus next in', () => {
		expect(scanStamp(at(-120), at(180), now)).toEqual([
			{ key: 'home.last_scan', vars: { rel: '2 min ago' } },
			{ key: 'scan.next_in', vars: { time: '3m' } }
		]);
	});
	it('no next: today text only', () => {
		expect(scanStamp(at(-120), null, now)).toEqual([{ key: 'home.last_scan', vars: { rel: '2 min ago' } }]);
		expect(scanStamp(null, null, now)).toEqual([{ key: 'home.last_scan', vars: { rel: 'never' } }]);
	});
	it('a next scan already due shows 1m, never 0m', () => {
		expect(scanStamp(at(-300), at(-5), now)[1].vars.time).toBe('1m');
	});
});
