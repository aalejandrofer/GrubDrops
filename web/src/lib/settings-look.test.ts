import { beforeEach, describe, expect, it } from 'vitest';
import type { Account, Canary, Runner, SettingsSSO, UpdateInfo } from '$lib/api/types';
import { setLang } from '$lib/i18n/index.svelte';
import { SECTIONS } from './settings';
import {
	SECTION_LOOK,
	failingCanaries,
	navDots,
	needLoginCount,
	passwordSwitch,
	ssoLine,
	updateChip
} from './settings-look';

beforeEach(() => setLang('en'));

const sso = (o: Partial<SettingsSSO> = {}): SettingsSSO => ({
	enabled: true,
	name: 'authentik',
	issuer_host: 'auth.example.com',
	allowed_emails: 0,
	allowed_groups: 0,
	...o
});

describe('section look', () => {
	it('gives every section a hue and its own icon', () => {
		for (const s of SECTIONS) expect(SECTION_LOOK[s].hue).toBeTruthy();
		expect(new Set(SECTIONS.map((s) => SECTION_LOOK[s].icon)).size).toBe(SECTIONS.length);
	});

	it('keeps red, amber and green for status, except Health', () => {
		const status = SECTIONS.filter((s) => ['red', 'amber', 'green'].includes(SECTION_LOOK[s].hue));
		expect(status).toEqual(['health']);
	});

	it('names the password section Authentication', () => {
		expect(SECTION_LOOK.password.label).toBe('settings.auth');
	});
});

describe('password sign-in switch', () => {
	it('has no switch without SSO and counts as on, whatever the stored setting', () => {
		for (const off of [false, true]) {
			const sw = passwordSwitch({ sso: sso({ enabled: false }), password_login_disabled: off });
			expect(sw).toEqual({ on: true, toggle: false, hint: 'Optional. Turn off once single sign-on is set up.' });
		}
	});

	it('can be turned off once SSO is set up', () => {
		expect(passwordSwitch({ sso: sso(), password_login_disabled: false })).toEqual({
			on: true,
			toggle: true,
			hint: 'Optional. Turn off to sign in with authentik only.'
		});
	});

	it('says how to get back in when it is off', () => {
		expect(passwordSwitch({ sso: sso(), password_login_disabled: true })).toEqual({
			on: false,
			toggle: true,
			hint: 'authentik only. If it is down, re-enable from the host.'
		});
	});
});

describe('single sign-on line', () => {
	it('offers providers when SSO is not set up', () => {
		expect(ssoLine(sso({ enabled: false }))).toBe('Sign in with authentik, Google or any OIDC provider.');
	});

	it('shows the issuer host and allowlist counts', () => {
		expect(ssoLine(sso({ allowed_emails: 2 }))).toBe('auth.example.com · 2 emails allowed · Set in env');
		expect(ssoLine(sso({ allowed_emails: 1, allowed_groups: 3 }))).toBe(
			'auth.example.com · 1 email allowed · 3 groups allowed · Set in env'
		);
		expect(ssoLine(sso({ allowed_groups: 1 }))).toBe('auth.example.com · 1 group allowed · Set in env');
	});

	it('flags an open door when there is no allowlist', () => {
		expect(ssoLine(sso())).toBe('auth.example.com · No allowlist · Set in env');
	});

	it('skips a missing host', () => {
		expect(ssoLine(sso({ issuer_host: '', allowed_emails: 2 }))).toBe('2 emails allowed · Set in env');
	});
});

describe('attention', () => {
	const acct = (id: string, o: Partial<Account> = {}) => ({ id, needs_login: false, enabled: true, ...o }) as Account;
	const run = (id: string, state: Runner['state']) => ({ account_id: id, state }) as Runner;

	it('counts accounts that need a login, from the account or its runner', () => {
		const accounts = [acct('a', { needs_login: true }), acct('b'), acct('c'), acct('d', { enabled: false })];
		expect(needLoginCount(accounts, [run('b', 'needs_login'), run('c', 'watching')])).toBe(2);
		expect(needLoginCount([], [])).toBe(0);
	});

	it('lists failing canaries, not skipped or passing ones', () => {
		const c = (platform: Canary['platform'], ok: boolean, skipped = false): Canary => ({
			platform,
			ok,
			skipped,
			detail: '',
			checked_at: '2026-11-01T00:00:00Z'
		});
		expect(failingCanaries([c('twitch', true), c('kick', false)])).toEqual(['kick']);
		expect(failingCanaries([c('twitch', false, true), c('kick', true)])).toEqual([]);
		expect(failingCanaries(undefined)).toEqual([]);
	});

	it('puts an amber dot on accounts and a red one on health', () => {
		expect(navDots({ needLogin: 0, canaryFailing: [] })).toEqual({});
		expect(navDots({ needLogin: 1, canaryFailing: ['kick'] })).toEqual({ accounts: 'warn', health: 'bad' });
	});
});

describe('update chip', () => {
	const u = (o: Partial<UpdateInfo>): UpdateInfo => ({
		current: 'v2.2.2',
		latest: 'v2.2.2',
		update_available: false,
		checked_at: '2026-11-01T00:00:00Z',
		notes: [],
		notes_url: '',
		command: '',
		auto: true,
		...o
	});

	it('says up to date or names the new version', () => {
		expect(updateChip(u({}))).toEqual({ tone: 'ok', text: 'Up to date' });
		expect(updateChip(u({ latest: 'v2.3.0', update_available: true }))).toEqual({ tone: 'info', text: 'v2.3.0 is available' });
	});

	it('shows nothing before a check', () => {
		expect(updateChip(null)).toBeNull();
		expect(updateChip(u({ latest: '' }))).toBeNull();
	});
});
