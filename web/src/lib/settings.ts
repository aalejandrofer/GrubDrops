import { clock } from '$lib/format';
import { t } from '$lib/i18n/index.svelte';

export const SECTIONS = ['general', 'accounts', 'notifications', 'health', 'updates'] as const;
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
