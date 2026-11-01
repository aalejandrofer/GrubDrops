import type { Account, Canary, Platform, Runner, Settings, UpdateInfo } from '$lib/api/types';
import { accountStatus } from '$lib/accounts';
import { t, tn } from '$lib/i18n/index.svelte';
import type { Section } from '$lib/settings';

/** Section hues; each maps to a `--h-<hue>` token in app.css. */
export type Hue = 'blue' | 'violet' | 'amber' | 'orange' | 'pink' | 'teal' | 'green' | 'red' | 'indigo';
export type SectionIcon = 'sliders' | 'users' | 'cpu' | 'flask' | 'bell' | 'pulse' | 'download' | 'lock';

/**
 * Each Settings section's label key, hue and icon, used on its nav link, heading, switches and focus rings.
 * Red, amber and green mean status (chips, dots), so only Health uses one of them as a section hue.
 */
export const SECTION_LOOK: Record<Section, { label: string; hue: Hue; icon: SectionIcon }> = {
	general: { label: 'settings.general', hue: 'blue', icon: 'sliders' },
	accounts: { label: 'settings.accounts', hue: 'violet', icon: 'users' },
	engine: { label: 'settings.engine', hue: 'orange', icon: 'cpu' },
	experimental: { label: 'settings.experimental', hue: 'pink', icon: 'flask' },
	notifications: { label: 'settings.notifications', hue: 'teal', icon: 'bell' },
	health: { label: 'settings.health', hue: 'green', icon: 'pulse' },
	updates: { label: 'settings.updates', hue: 'blue', icon: 'download' },
	password: { label: 'settings.auth', hue: 'indigo', icon: 'lock' }
};

/**
 * The password sign-in row. Without SSO the server always accepts the password, so the
 * row shows no switch (`toggle` false) and the password counts as on whatever is stored.
 */
export function passwordSwitch(s: Pick<Settings, 'sso' | 'password_login_disabled'>): {
	on: boolean;
	toggle: boolean;
	hint: string;
} {
	if (!s.sso.enabled) return { on: true, toggle: false, hint: t('settings.password_login_nosso_sub') };
	if (s.password_login_disabled) return { on: false, toggle: true, hint: t('settings.password_login_off_sub', { name: s.sso.name }) };
	return { on: true, toggle: true, hint: t('settings.password_login_on_sub', { name: s.sso.name }) };
}

/** "auth.example.com · 2 emails allowed · Set in env", or the pitch when SSO is not set up. */
export function ssoLine(sso: Settings['sso']): string {
	if (!sso.enabled) return t('settings.sso_off_sub');
	const parts: string[] = [];
	if (sso.issuer_host) parts.push(sso.issuer_host);
	if (sso.allowed_emails > 0) parts.push(tn('settings.sso_emails', sso.allowed_emails));
	if (sso.allowed_groups > 0) parts.push(tn('settings.sso_groups', sso.allowed_groups));
	if (sso.allowed_emails === 0 && sso.allowed_groups === 0) parts.push(t('settings.sso_no_allowlist'));
	parts.push(t('settings.sso_env'));
	return parts.join(' · ');
}

/** Accounts that show "Signed out" in the list. */
export function needLoginCount(accounts: Account[], runners: Runner[]): number {
	return accounts.filter(
		(a) =>
			accountStatus(
				a,
				runners.find((r) => r.account_id === a.id)
			) === 'signed_out'
	).length;
}

/** Platforms whose last canary check failed. Skipped and passing checks don't count. */
export function failingCanaries(canary: Canary[] | undefined): Platform[] {
	return (canary ?? []).filter((c) => !c.ok && !c.skipped).map((c) => c.platform);
}

/** Nav link dots: amber when a section needs the user, red when something is failing. */
export function navDots(a: { needLogin: number; canaryFailing: Platform[] }): Partial<Record<Section, 'warn' | 'bad'>> {
	const out: Partial<Record<Section, 'warn' | 'bad'>> = {};
	if (a.needLogin > 0) out.accounts = 'warn';
	if (a.canaryFailing.length > 0) out.health = 'bad';
	return out;
}

/** Updates heading chip, or null until a check has run. */
export function updateChip(u: UpdateInfo | null): { tone: 'ok' | 'info'; text: string } | null {
	if (!u?.latest) return null;
	if (u.update_available) return { tone: 'info', text: t('version.available', { v: u.latest }) };
	return { tone: 'ok', text: t('settings.up_to_date') };
}
