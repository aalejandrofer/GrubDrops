import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Runner } from '$lib/api/types';
import runnersMining from '$lib/fixtures/runners.mining.json';
import {
	inProgress,
	pageCount,
	pageQuery,
	pageRange,
	readPageSize,
	validPageSize,
	writePageSize
} from './inventory';

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

describe('pageQuery', () => {
	it('offsets by page and size', () => {
		expect(pageQuery({ game: 'Rust', q: 'hat' }, 3, 50)).toEqual({ game: 'Rust', q: 'hat', limit: 50, offset: 100 });
		expect(pageQuery({}, 1, 20)).toEqual({ limit: 20, offset: 0 });
	});
});

describe('pageCount', () => {
	it('is at least 1', () => {
		expect(pageCount(0, 20)).toBe(1);
		expect(pageCount(20, 20)).toBe(1);
		expect(pageCount(21, 20)).toBe(2);
		expect(pageCount(805, 100)).toBe(9);
	});
});

describe('pageRange', () => {
	it('gives from and to', () => {
		expect(pageRange(1, 20, 0)).toEqual({ from: 0, to: 0 });
		expect(pageRange(1, 20, 7)).toEqual({ from: 1, to: 7 });
		expect(pageRange(2, 20, 45)).toEqual({ from: 21, to: 40 });
		expect(pageRange(3, 20, 45)).toEqual({ from: 41, to: 45 });
	});
});

describe('page size storage', () => {
	afterEach(() => vi.unstubAllGlobals());
	const stub = (v: string | null) =>
		vi.stubGlobal('localStorage', { getItem: () => v, setItem: vi.fn() });
	it('accepts 20, 50, 100', () => {
		for (const n of [20, 50, 100]) expect(validPageSize(n)).toBe(n);
	});
	it('falls back to 20', () => {
		for (const v of [null, '', 'abc', '10', '75']) {
			stub(v);
			expect(readPageSize()).toBe(20);
		}
		stub('50');
		expect(readPageSize()).toBe(50);
	});
	it('survives throwing storage', () => {
		vi.stubGlobal('localStorage', {
			getItem: () => {
				throw new Error('blocked');
			},
			setItem: () => {
				throw new Error('blocked');
			}
		});
		expect(readPageSize()).toBe(20);
		expect(() => writePageSize(50)).not.toThrow();
	});
});
