import { beforeEach, describe, expect, it } from 'vitest';
import { setLang } from '$lib/i18n/index.svelte';
import { readFileSync } from 'node:fs';
import { SECTIONS, isDiscordWebhook, sectionFromHash, timeExample, zoneOptions } from './settings';

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

describe('discord webhook', () => {
	it('accepts only discord webhook URLs', () => {
		expect(isDiscordWebhook('https://discord.com/api/webhooks/1/abc')).toBe(true);
		expect(isDiscordWebhook('https://discordapp.com/api/webhooks/1/abc')).toBe(true);
		expect(isDiscordWebhook('hunter2-admin-password')).toBe(false);
		expect(isDiscordWebhook('http://discord.com/api/webhooks/1/abc')).toBe(false);
		expect(isDiscordWebhook('https://discord.com/api/webhooks/')).toBe(false);
		expect(isDiscordWebhook('https://evil.example/?https://discord.com/api/webhooks/1')).toBe(false);
	});
	it('the input is not a password field and never saves on blur', () => {
		const src = readFileSync('src/routes/(app)/settings/Notifications.svelte', 'utf8');
		expect(src).not.toContain('type="password"');
		expect(src).not.toMatch(/onblur|onchange|oninput=\{commit/);
		expect(src).not.toMatch(/id="[^"]*password/i);
		for (const attr of ['autocomplete="off"', 'data-1p-ignore', 'data-lpignore="true"', 'data-bwignore']) {
			expect(src).toContain(attr);
		}
	});
	it('General selects save only on a trusted change', () => {
		const src = readFileSync('src/routes/(app)/settings/General.svelte', 'utf8');
		const changes = src.match(/onchange=/g) ?? [];
		const guarded = src.match(/if \(e\.isTrusted && v !== s\.\w+\)/g) ?? [];
		expect(changes.length).toBeGreaterThan(0);
		expect(guarded.length).toBe(changes.length);
	});
});
