import { describe, expect, it } from 'vitest';
import type { Account } from '$lib/api/types';
import { canContinue, segments, signedIn, startStep, wantsWizard } from './onboarding';

const acct = (id: string, needs_login: boolean): Account => ({
	id,
	platform: 'twitch',
	name: id,
	platform_name: id,
	avatar_url: '',
	state: 'idle',
	needs_login,
	enabled: true,
	own_games: false,
	own_game_count: 0,
	auth: null
});

describe('wantsWizard', () => {
	it('only for first run', () => {
		expect(wantsWizard('first_run')).toBe(true);
		for (const v of ['loading', 'error', 'offline', 'no_games', 'paused', 'signed_out', 'nothing_live', 'healthy'] as const)
			expect(wantsWizard(v)).toBe(false);
	});
});

describe('startStep', () => {
	it('opens on accounts with none signed in', () => {
		expect(startStep(0, 0)).toBe(1);
		expect(startStep(0, 3)).toBe(1);
	});
	it('opens on games with an account and no games', () => {
		expect(startStep(1, 0)).toBe(2);
	});
	it('a half-made account alone stays on accounts', () => {
		const n = signedIn([acct('half', true)]).length;
		expect(startStep(n, 0)).toBe(1);
	});
	it('several accounts and no games opens on games', () => {
		expect(startStep(3, 0)).toBe(2);
	});
	it('sends a set-up user home', () => {
		expect(startStep(2, 1)).toBeNull();
	});
});

describe('signedIn', () => {
	it('skips accounts that need a login', () => {
		expect(signedIn([acct('a', false), acct('b', true)]).map((a) => a.id)).toEqual(['a']);
	});
});

describe('canContinue', () => {
	it('step 1 needs a signed-in account', () => {
		expect(canContinue(1, { signedIn: 0, games: 5 })).toBe(false);
		expect(canContinue(1, { signedIn: 1, games: 0 })).toBe(true);
	});
	it('step 2 needs a game', () => {
		expect(canContinue(2, { signedIn: 1, games: 0 })).toBe(false);
		expect(canContinue(2, { signedIn: 1, games: 1 })).toBe(true);
	});
});

describe('segments', () => {
	it('marks the current step', () => {
		expect(segments(1)).toEqual(['cur', 'todo']);
		expect(segments(2)).toEqual(['done', 'cur']);
	});
});
