import { describe, expect, it } from 'vitest';
import { runNowState } from './canary';

const before = { twitch: '2026-10-25T11:00:00Z', kick: null };
const c = (platform: string, checked_at: string | null) => ({ platform, checked_at });

describe('runNowState', () => {
	it('unchanged checked_at inside 90 s: running', () => {
		expect(runNowState(before, [c('twitch', '2026-10-25T11:00:00Z'), c('kick', null)], 89_000)).toBe('running');
	});
	it('a changed checked_at: done', () => {
		expect(runNowState(before, [c('twitch', '2026-10-25T11:00:05Z')], 5000)).toBe('done');
	});
	it('server clock behind the browser: a new but earlier-looking checked_at is still done', () => {
		expect(runNowState({ twitch: '2026-10-25T11:00:00Z' }, [c('twitch', '2026-10-25T11:00:07Z')], 5000)).toBe('done');
	});
	it('null before, non-null now: done', () => {
		expect(runNowState(before, [c('twitch', '2026-10-25T11:00:00Z'), c('kick', '2026-10-25T12:00:00Z')], 5000)).toBe('done');
	});
	it('platform with no snapshot entry and a result: done', () => {
		expect(runNowState({}, [c('kick', '2026-10-25T12:00:00Z')], 5000)).toBe('done');
	});
	it('nothing changed at 90 s: no_result', () => {
		expect(runNowState(before, [], 90_000)).toBe('no_result');
	});
	it('a change at 90 s still counts as done', () => {
		expect(runNowState(before, [c('kick', '2026-10-25T12:00:00Z')], 90_000)).toBe('done');
	});
});
