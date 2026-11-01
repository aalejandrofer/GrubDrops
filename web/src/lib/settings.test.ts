import { beforeEach, describe, expect, it } from 'vitest';
import { setLang, t } from '$lib/i18n/index.svelte';
import { readFileSync } from 'node:fs';
import {
	SECTIONS,
	isAvatarURL,
	isChannel,
	isClock,
	isDiscordWebhook,
	isMentionID,
	isProxyURL,
	minutesLabel,
	isTelegramChat,
	isTelegramToken,
	passwordErrorKey,
	passwordProblem,
	sectionFromHash,
	shareCatalogToggle,
	sleepProblem,
	timeExample,
	zoneOptions
} from './settings';

beforeEach(() => setLang('en'));

describe('settings helpers', () => {
	it('sectionFromHash maps known hashes and falls back to general', () => {
		expect(sectionFromHash('#health')).toBe('health');
		expect(sectionFromHash('#password')).toBe('password');
		expect(sectionFromHash('#nope')).toBe('general');
		expect(sectionFromHash('')).toBe('general');
	});

	it('SECTIONS order is general first and password last, under updates', () => {
		expect([...SECTIONS]).toEqual(['general', 'accounts', 'engine', 'experimental', 'notifications', 'health', 'updates', 'password']);
	});

	it('sectionFromHash knows the experimental section', () => {
		expect(sectionFromHash('#experimental')).toBe('experimental');
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

describe('telegram', () => {
	it('accepts bot tokens and chat IDs the server accepts', () => {
		expect(isTelegramToken('123456:ABC-def_789')).toBe(true);
		expect(isTelegramToken('ABC-def')).toBe(false);
		expect(isTelegramToken('123:abc/x')).toBe(false);
		expect(isTelegramChat('-1001234567890')).toBe(true);
		expect(isTelegramChat('42')).toBe(true);
		expect(isTelegramChat('@grub_alerts')).toBe(true);
		expect(isTelegramChat('@x')).toBe(false);
		expect(isTelegramChat('chat')).toBe(false);
	});
});

describe('mention IDs', () => {
	it('accepts digits only or empty', () => {
		expect(isMentionID('')).toBe(true);
		expect(isMentionID('123456789012345678')).toBe(true);
		expect(isMentionID('987654321')).toBe(true);
		expect(isMentionID('<@123>')).toBe(false);
		expect(isMentionID('@grub_user')).toBe(false);
		expect(isMentionID('-100123')).toBe(false);
		expect(isMentionID('123456789012345678901')).toBe(false);
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

describe('experimental settings', () => {
	it('the share toggle works with an Android Twitch login', () => {
		const st = shareCatalogToggle({ share_catalog_available: true });
		expect(st.disabled).toBe(false);
		expect(t(st.hint)).toBe('TV logins also mine campaigns an Android login can see.');
	});
	it('without an Android and a TV Twitch login it is disabled and says why', () => {
		const st = shareCatalogToggle({ share_catalog_available: false });
		expect(st.disabled).toBe(true);
		expect(t(st.hint)).toBe('Needs an Android and a TV Twitch login.');
	});
	it('the section shows a chip when the helper says unavailable and saves only its own field', () => {
		const src = readFileSync('src/routes/(app)/settings/Experimental.svelte', 'utf8');
		expect(src).toContain('id="experimental"');
		expect(src).toContain('{#if share.disabled}');
		expect(src).toContain("<span class=\"chip off\">{t('settings.unavailable')}</span>");
		expect(src).toContain('class:na={share.disabled}');
		expect(src).toContain("save('exp_share_catalog', { exp_share_catalog: !s.exp_share_catalog })");
	});
	it('the page renders Experimental right after Engine', () => {
		const src = readFileSync('src/routes/(app)/settings/+page.svelte', 'utf8');
		const eng = src.indexOf('<Engine {s}');
		const exp = src.indexOf('<Experimental {s}');
		const notif = src.indexOf('<Notifications {s}');
		expect(eng).toBeGreaterThan(-1);
		expect(exp).toBeGreaterThan(eng);
		expect(notif).toBeGreaterThan(exp);
	});
});

describe('engine settings helpers', () => {
	it('minutesLabel shows minutes below an hour and hours above', () => {
		expect(minutesLabel(15)).toBe('15 min');
		expect(minutesLabel(60)).toBe('1 hour');
		expect(minutesLabel(1440)).toBe('24 hours');
		expect(minutesLabel(90)).toBe('90 min');
	});

	it('isProxyURL takes http, https and socks5 with a host, or empty', () => {
		expect(isProxyURL('')).toBe(true);
		expect(isProxyURL('http://10.0.0.1:3128')).toBe(true);
		expect(isProxyURL('socks5://user:pass@127.0.0.1:1080')).toBe(true);
		expect(isProxyURL('ftp://10.0.0.1')).toBe(false);
		expect(isProxyURL('10.0.0.1:3128')).toBe(false);
		expect(isProxyURL('socks5://')).toBe(false);
	});

	it('isClock takes a 24-hour HH:MM only', () => {
		for (const v of ['00:00', '02:00', '09:59', '23:59']) expect(isClock(v)).toBe(true);
		for (const v of ['', '2:00', '24:00', '12:60', '12:5', '12-30', '12:30:00', ' 12:30']) expect(isClock(v)).toBe(false);
	});

	it('sleepProblem rejects bad and equal times, and allows crossing midnight', () => {
		expect(sleepProblem('02:00', '08:00')).toBeNull();
		expect(sleepProblem('23:00', '07:00')).toBeNull();
		expect(sleepProblem('', '08:00')).toBe('settings.sleep_invalid');
		expect(sleepProblem('02:00', '25:00')).toBe('settings.sleep_invalid');
		expect(sleepProblem('03:30', '03:30')).toBe('settings.sleep_same');
	});

	it('isChannel takes up to 25 word characters', () => {
		expect(isChannel('')).toBe(true);
		expect(isChannel('alveus_sanctuary')).toBe(true);
		expect(isChannel('a'.repeat(26))).toBe(false);
		expect(isChannel('bad name')).toBe(false);
	});

	it('isAvatarURL takes https only, or empty', () => {
		expect(isAvatarURL('')).toBe(true);
		expect(isAvatarURL('https://cdn.example.com/a.png')).toBe(true);
		expect(isAvatarURL('http://cdn.example.com/a.png')).toBe(false);
	});
});

describe('change password', () => {
	it('passwordProblem checks length, then the confirm field', () => {
		expect(passwordProblem('short', 'short')).toBe('auth.too_short');
		expect(passwordProblem('longenough', 'different1')).toBe('auth.mismatch');
		expect(passwordProblem('longenough', 'longenough')).toBeNull();
	});
	it('passwordErrorKey maps API codes and falls back', () => {
		expect(passwordErrorKey('wrong_password')).toBe('auth.wrong_password');
		expect(passwordErrorKey('password_short')).toBe('auth.too_short');
		expect(passwordErrorKey('internal')).toBe('auth.failed');
		expect(passwordErrorKey(undefined)).toBe('auth.failed');
	});
});
