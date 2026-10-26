import { describe, expect, it } from 'vitest';
import type { Runner } from '$lib/api/types';
import runnersMining from '$lib/fixtures/runners.mining.json';
import { inProgress, nextQuery, queuedCount } from './rewards';

const R = () => structuredClone(runnersMining) as Runner[];

describe('inProgress', () => {
	it('sorts by pct desc', () => {
		const p = inProgress(R());
		expect(p.map((x) => x.pct)).toEqual([70, 30]);
		expect(p.some((x) => x.runner.state === 'needs_login')).toBe(false);
	});
	it('left minutes', () => {
		expect(inProgress(R())[0].left).toBe(18);
	});
});

describe('queuedCount', () => {
	it('sums up_next', () => {
		expect(queuedCount(R())).toBe(4);
	});
});

describe('nextQuery', () => {
	it('pages by 10', () => {
		expect(nextQuery({ game: 'Rust', account: 'a' }, 10)).toEqual({
			game: 'Rust',
			account: 'a',
			limit: 10,
			offset: 10
		});
	});
});
