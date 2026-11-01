import { afterEach, describe, expect, it, vi } from 'vitest';
import { advOpen, advanced, advancedIn, EXPAND_ADV_KEY, readExpandAdv, setExpandAdv } from './advanced.svelte';

describe('advOpen', () => {
	it('opens every section while the pref is on', () => {
		expect(advOpen(true, false)).toBe(true);
		expect(advOpen(true, true)).toBe(true);
	});
	it('follows the user while the pref is off', () => {
		expect(advOpen(false, false)).toBe(false);
		expect(advOpen(false, true)).toBe(true);
	});
});

describe('advancedIn', () => {
	it('finds the node itself and sections inside it', () => {
		const inner = { open: false };
		const self = { open: false, matches: (s: string) => s === 'details.adv', querySelectorAll: () => [inner] };
		expect(advancedIn(self)).toEqual([self, inner]);
	});
	it('skips text nodes and other elements', () => {
		expect(advancedIn({})).toEqual([]);
		expect(advancedIn({ matches: () => false, querySelectorAll: () => [] })).toEqual([]);
	});
});

describe('expand pref storage', () => {
	afterEach(() => {
		vi.unstubAllGlobals();
		advanced.expand = false;
	});

	it('reads on only for the stored flag', () => {
		vi.stubGlobal('localStorage', { getItem: (k: string) => (k === EXPAND_ADV_KEY ? '1' : null) });
		expect(readExpandAdv()).toBe(true);
		vi.stubGlobal('localStorage', { getItem: () => null });
		expect(readExpandAdv()).toBe(false);
	});

	it('reads off when storage throws', () => {
		vi.stubGlobal('localStorage', {
			getItem: () => {
				throw new Error('blocked');
			}
		});
		expect(readExpandAdv()).toBe(false);
	});

	it('stores on and clears off', () => {
		const setItem = vi.fn();
		const removeItem = vi.fn();
		vi.stubGlobal('localStorage', { setItem, removeItem });
		setExpandAdv(true);
		expect(advanced.expand).toBe(true);
		expect(setItem).toHaveBeenCalledWith(EXPAND_ADV_KEY, '1');
		setExpandAdv(false);
		expect(advanced.expand).toBe(false);
		expect(removeItem).toHaveBeenCalledWith(EXPAND_ADV_KEY);
	});

	it('keeps the choice for the visit when storage throws', () => {
		vi.stubGlobal('localStorage', {
			setItem: () => {
				throw new Error('full');
			}
		});
		setExpandAdv(true);
		expect(advanced.expand).toBe(true);
	});
});
