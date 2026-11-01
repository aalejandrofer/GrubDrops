import { describe, expect, it } from 'vitest';
import { newIn } from './nav';

describe('newIn', () => {
	it('shows on every 2.3 build', () => {
		expect(newIn('2.3.0', '2.3')).toBe(true);
		expect(newIn('v2.3.4', '2.3')).toBe(true);
		expect(newIn('2.3.0-staging', '2.3')).toBe(true);
	});
	it('hides on other versions', () => {
		expect(newIn('2.4.0', '2.3')).toBe(false);
		expect(newIn('2.2.2', '2.3')).toBe(false);
		expect(newIn('12.3.0', '2.3')).toBe(false);
		expect(newIn('dev', '2.3')).toBe(false);
		expect(newIn('', '2.3')).toBe(false);
	});
});
