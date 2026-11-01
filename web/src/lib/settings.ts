import type { Settings } from '$lib/api/types';
import { clock } from '$lib/format';
import { t, tn } from '$lib/i18n/index.svelte';

export const SECTIONS = ['general', 'accounts', 'engine', 'experimental', 'notifications', 'health', 'updates', 'password'] as const;
export type Section = (typeof SECTIONS)[number];

export function sectionFromHash(hash: string): Section {
	const h = hash.replace(/^#/, '');
	return (SECTIONS as readonly string[]).includes(h) ? (h as Section) : 'general';
}

/** The server default first, then every IANA zone the runtime knows. */
export function zoneOptions(effective: string): { value: string; label: string }[] {
	const zones = typeof Intl.supportedValuesOf === 'function' ? Intl.supportedValuesOf('timeZone') : [];
	return [
		{ value: '', label: t('settings.tz_default', { zone: effective }) },
		...zones.map((z) => ({ value: z, label: z }))
	];
}

export function timeExample(fmt: '24h' | '12h', zone: string, now: number): string {
	return clock(new Date(now).toISOString(), zone, fmt);
}

/** Same prefixes as the server's validWebhook. */
export const WEBHOOK_PREFIXES = ['https://discord.com/api/webhooks/', 'https://discordapp.com/api/webhooks/'];

export function isDiscordWebhook(v: string): boolean {
	return WEBHOOK_PREFIXES.some((p) => v.startsWith(p) && v.length > p.length);
}

/** Same pattern as the server's telegramTokenRe. */
export function isTelegramToken(v: string): boolean {
	return /^[0-9]+:[A-Za-z0-9_-]+$/.test(v);
}

/** Same pattern as the server's telegramChatRe: a numeric ID or an @channel name. */
export function isTelegramChat(v: string): boolean {
	return /^(-?[0-9]+|@[A-Za-z0-9_]{5,})$/.test(v);
}

/** Same rule as the server's validMentionID: digits only, or empty to turn the mention off. */
export function isMentionID(v: string): boolean {
	return v === '' || /^[0-9]{1,20}$/.test(v);
}

/** The share toggle works only with an Android and a TV Twitch login; without both it is disabled and says why. */
export function shareCatalogToggle(s: Pick<Settings, 'share_catalog_available'>): { disabled: boolean; hint: string } {
	return s.share_catalog_available
		? { disabled: false, hint: 'settings.exp_share_catalog_sub' }
		: { disabled: true, hint: 'settings.exp_share_catalog_needs' };
}

/** Minutes the server accepts for scan_interval_min. */
export const SCAN_INTERVALS = [15, 30, 60, 180, 360, 720, 1440];

/** Canary intervals offered, in seconds; 0 turns the canary off. */
export const CANARY_INTERVALS = [0, 3600, 3 * 3600, 6 * 3600, 12 * 3600, 24 * 3600];

/** "15 min", "1 hour", "24 hours". */
export function minutesLabel(min: number): string {
	return min % 60 === 0 ? tn('settings.n_hours', min / 60) : tn('settings.n_minutes', min);
}

/** Same rule as the server: http, https or socks5 with a host. Empty is allowed. */
export function isProxyURL(v: string): boolean {
	if (v === '') return true;
	try {
		const u = new URL(v);
		return ['http:', 'https:', 'socks5:'].includes(u.protocol) && u.hostname !== '';
	} catch {
		return false;
	}
}

/** Same rule as the server's ParseClock: a 24-hour "HH:MM". */
export function isClock(v: string): boolean {
	return /^([01][0-9]|2[0-3]):[0-5][0-9]$/.test(v);
}

/** i18n key for sleep window times the server would reject, or null when they can go. */
export function sleepProblem(start: string, end: string): string | null {
	if (!isClock(start) || !isClock(end)) return 'settings.sleep_invalid';
	if (start === end) return 'settings.sleep_same';
	return null;
}

/** A Twitch or Kick channel login: up to 25 letters, digits or underscores. Empty is allowed. */
export function isChannel(v: string): boolean {
	return /^[A-Za-z0-9_]{0,25}$/.test(v);
}

/** Same rule as the server: an https URL. Empty is allowed. */
export function isAvatarURL(v: string): boolean {
	if (v === '') return true;
	try {
		const u = new URL(v);
		return u.protocol === 'https:' && u.hostname !== '';
	} catch {
		return false;
	}
}

/** Same minimum as the server. */
export const PASSWORD_MIN = 8;

/** i18n key for a new password the form should not send, or null when it can go. */
export function passwordProblem(next: string, confirm: string): string | null {
	if (next.length < PASSWORD_MIN) return 'auth.too_short';
	if (next !== confirm) return 'auth.mismatch';
	return null;
}

const PASSWORD_ERRORS = new Map([
	['wrong_password', 'auth.wrong_password'],
	['password_short', 'auth.too_short']
]);

/** i18n key for a change-password error code from the API. */
export function passwordErrorKey(code: string | undefined): string {
	return (code && PASSWORD_ERRORS.get(code)) || 'auth.failed';
}
