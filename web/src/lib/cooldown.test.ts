import { describe, expect, it } from 'vitest';
import { refreshLabel } from './cooldown';

describe('refreshLabel', () => {
	it('busy shows Refreshing, done and idle show the idle key', () => {
		expect(refreshLabel(true, false, 'scan.refresh')).toBe('points.refreshing');
		expect(refreshLabel(true, true, 'scan.refresh')).toBe('points.refreshing');
		expect(refreshLabel(false, true, 'scan.refresh')).toBe('scan.refresh');
		expect(refreshLabel(false, false, 'points.refresh_now')).toBe('points.refresh_now');
	});
});
