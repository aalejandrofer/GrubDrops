import { describe, expect, it } from 'vitest';
import { isInteractive, pastThreshold, targetIndex } from './drag';

const rect = (top: number, h = 100) => ({ top, bottom: top + h });
const rows = [rect(0), rect(100), rect(200), rect(300)];

describe('pastThreshold', () => {
	it('ignores small moves', () => {
		expect(pastThreshold(0, 0)).toBe(false);
		expect(pastThreshold(2, 2)).toBe(false);
		expect(pastThreshold(0, 3.9)).toBe(false);
	});
	it('starts at 4px in any direction', () => {
		expect(pastThreshold(0, 4)).toBe(true);
		expect(pastThreshold(-4, 0)).toBe(true);
		expect(pastThreshold(3, 3)).toBe(true);
	});
});

describe('targetIndex', () => {
	it('stays put while over the dragged row', () => {
		expect(targetIndex(250, rows, 2)).toBe(2);
		expect(targetIndex(210, rows, 2)).toBe(2);
		expect(targetIndex(290, rows, 2)).toBe(2);
	});
	it('moves up past a midpoint', () => {
		expect(targetIndex(140, rows, 2)).toBe(1);
		expect(targetIndex(40, rows, 2)).toBe(0);
		expect(targetIndex(120, rows, 3)).toBe(1);
	});
	it('moves down past a midpoint', () => {
		expect(targetIndex(160, rows, 0)).toBe(1);
		expect(targetIndex(260, rows, 0)).toBe(2);
		expect(targetIndex(999, rows, 0)).toBe(3);
	});
	it('clamps above and below the list', () => {
		expect(targetIndex(-50, rows, 3)).toBe(0);
		expect(targetIndex(5000, rows, 0)).toBe(3);
	});
});

describe('isInteractive', () => {
	const fake = (tag: string | null) => ({
		closest: (sel: string) => (tag && sel.split(',').map((s) => s.trim()).includes(tag) ? {} : null)
	});
	it('skips controls', () => {
		for (const t of ['button', 'a', 'input', 'select', 'textarea', 'label', '[role="menu"]']) {
			expect(isInteractive(fake(t))).toBe(true);
		}
	});
	it('skips a campaign link, so a click opens the drawer instead of dragging', () => {
		expect(isInteractive(fake('[data-campaign]'))).toBe(true);
	});
	it('allows plain content', () => {
		expect(isInteractive(fake(null))).toBe(false);
	});
	it('skips a missing target', () => {
		expect(isInteractive(null)).toBe(true);
	});
});
