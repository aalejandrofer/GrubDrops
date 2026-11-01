import { describe, expect, it } from 'vitest';
import type { Account, PointsResp, Runner } from '$lib/api/types';
import accountsFx from '$lib/fixtures/accounts.json';
import pointsFx from '$lib/fixtures/points.json';
import runnersMining from '$lib/fixtures/runners.mining.json';
import { fieldKey, menuActions, mineBlock, nextDrop, pointsTotal } from './account-panel';

const ACC = (id: string) => structuredClone(accountsFx.find((a) => a.id === id)) as Account;
const RUN = (id: string) => structuredClone(runnersMining.find((r) => r.account_id === id)) as Runner;

describe('menuActions', () => {
	it('an active account offers pause and restart, never remove', () => {
		expect(menuActions({ enabled: true })).toEqual(['details', 'check', 'pause', 'restart', 'skip_sleep']);
	});
	it('a paused account offers resume and no restart', () => {
		expect(menuActions({ enabled: false })).toEqual(['details', 'check', 'resume', 'skip_sleep']);
	});
});

describe('nextDrop', () => {
	it('is the first Up next item', () => {
		const r = RUN('acc-hjune');
		expect(nextDrop(r)?.benefit_id).toBe(r.up_next[0].benefit_id);
	});
	it('is null without a runner or a queue', () => {
		expect(nextDrop(undefined)).toBeNull();
		expect(nextDrop(RUN('acc-kick'))).toBeNull();
	});
});

describe('mineBlock', () => {
	it('a watching account with a queue can mine now', () => {
		expect(mineBlock(ACC('acc-hjune'), RUN('acc-hjune'))).toBeNull();
	});
	it('signed out wins over paused', () => {
		const a = ACC('acc-kick');
		a.enabled = false;
		expect(mineBlock(a, RUN('acc-kick'))).toBe('accounts.login_first');
		const b = ACC('acc-hjune');
		expect(mineBlock(b, { ...RUN('acc-hjune'), state: 'needs_login' })).toBe('accounts.login_first');
	});
	it('a paused account must resume first', () => {
		const a = ACC('acc-hjune');
		a.enabled = false;
		expect(mineBlock(a, RUN('acc-hjune'))).toBe('accounts.resume_first');
	});
	it('an empty queue has nothing to switch to', () => {
		expect(mineBlock(ACC('acc-hjune'), { ...RUN('acc-hjune'), up_next: [] })).toBe('accounts.next_none');
		expect(mineBlock(ACC('acc-hjune'), undefined)).toBe('accounts.next_none');
	});
	it('the sleep window and idle states block it', () => {
		expect(mineBlock(ACC('acc-hjune'), { ...RUN('acc-hjune'), state: 'sleep_window' })).toBe('upnext.mine_sleep_window');
		expect(mineBlock(ACC('acc-hjune'), { ...RUN('acc-hjune'), state: 'no_games' })).toBe('accounts.mine_idle');
	});
});

describe('pointsTotal', () => {
	const p = pointsFx as PointsResp;
	it('reads the account total from GET /api/points', () => {
		expect(pointsTotal(p, 't1')).toBe(16340);
	});
	it('is null when unknown, unloaded or missing', () => {
		expect(pointsTotal(p, 'k1')).toBeNull();
		expect(pointsTotal(null, 't1')).toBeNull();
		expect(pointsTotal(p, 'nope')).toBeNull();
	});
});

it('fieldKey joins the account and field', () => {
	expect(fieldKey('t1', 'points')).toBe('t1:points');
});
