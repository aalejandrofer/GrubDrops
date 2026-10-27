import { expect, test } from 'vitest';
import type { Settings } from '$lib/api/types';
import settings from '$lib/fixtures/settings.json';
import { applyPrefs, prefs, usableZone } from './prefs.svelte';

test('usableZone keeps IANA zones and maps the rest to UTC', () => {
	expect(usableZone('Europe/Madrid')).toBe('Europe/Madrid');
	expect(usableZone('Local')).toBe('UTC');
	expect(usableZone('Not/AZone')).toBe('UTC');
});

test('applyPrefs never stores a zone Intl rejects', () => {
	const s = { ...(structuredClone(settings) as Settings), timezone: 'Local' };
	applyPrefs(s);
	expect(prefs.zone).toBe('UTC');
});

test('applyPrefs carries the drop priority mode for Up next and Games', () => {
	const s = structuredClone(settings) as Settings;
	applyPrefs({ ...s, priority_mode: 'ending_soonest' });
	expect(prefs.priorityMode).toBe('ending_soonest');
	applyPrefs({ ...s, priority_mode: 'ordered' });
	expect(prefs.priorityMode).toBe('ordered');
});
