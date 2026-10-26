import { beforeEach, describe, expect, it } from 'vitest';
import { setLang } from '$lib/i18n/index.svelte';
import { SECTIONS, sectionFromHash, timeExample, zoneOptions } from './settings';

beforeEach(() => setLang('en'));

describe('settings helpers', () => {
	it('sectionFromHash maps known hashes and falls back to general', () => {
		expect(sectionFromHash('#health')).toBe('health');
		expect(sectionFromHash('#nope')).toBe('general');
		expect(sectionFromHash('')).toBe('general');
	});

	it('SECTIONS order is general first', () => {
		expect([...SECTIONS]).toEqual(['general', 'accounts', 'notifications', 'health', 'updates']);
	});

	it('zoneOptions starts with server default', () => {
		const o = zoneOptions('Asia/Shanghai');
		expect(o[0]).toEqual({ value: '', label: 'Server default (Asia/Shanghai)' });
		expect(o.some((z) => z.value === 'Europe/Madrid')).toBe(true);
	});

	it('timeExample formats both clocks', () => {
		const now = Date.UTC(2026, 9, 25, 9, 12);
		expect(timeExample('24h', 'UTC', now)).toBe('09:12');
		expect(timeExample('12h', 'UTC', now)).toBe('9:12 AM');
		expect(timeExample('24h', 'Asia/Shanghai', now)).toBe('17:12');
	});
});
