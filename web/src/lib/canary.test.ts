import { describe, expect, it } from 'vitest';
import { runNowState } from './canary';

const click = Date.parse('2026-10-25T12:00:00Z');
const at = (s: number) => new Date(click + s * 1000).toISOString();
const c = (checked_at: string | null) => ({ checked_at });

describe('runNowState', () => {
	it('no newer result inside 90 s: running', () => {
		expect(runNowState(click, [c(at(-60)), c(null)], click + 89_000)).toBe('running');
	});
	it('any canary newer than the click: done', () => {
		expect(runNowState(click, [c(at(-60)), c(at(4))], click + 5000)).toBe('done');
	});
	it('nothing new at 90 s: no_result', () => {
		expect(runNowState(click, [], click + 90_000)).toBe('no_result');
	});
	it('a result at 90 s still counts as done', () => {
		expect(runNowState(click, [c(at(10))], click + 90_000)).toBe('done');
	});
});
