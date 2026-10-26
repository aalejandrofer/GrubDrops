import type { Settings } from '$lib/api/types';
import { setLang } from '$lib/i18n/index.svelte';

/** Display preferences from settings: clock format and time zone for formatters. */
export const prefs: { zone: string; fmt: '24h' | '12h' } = $state({
	zone: typeof Intl === 'undefined' ? 'UTC' : Intl.DateTimeFormat().resolvedOptions().timeZone,
	fmt: '24h'
});

export function applyPrefs(s: Settings): void {
	setLang(s.language);
	if (typeof document !== 'undefined') document.documentElement.dataset.theme = s.theme;
	prefs.fmt = s.time_format;
	prefs.zone = s.timezone || s.zone_effective || prefs.zone;
}
