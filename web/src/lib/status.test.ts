import { beforeEach, describe, expect, it } from 'vitest';
import type { Overview } from '$lib/api/types';
import { setLang } from '$lib/i18n/index.svelte';
import firstRun from '$lib/fixtures/overview.first_run.json';
import mining from '$lib/fixtures/overview.mining.json';
import nothingLive from '$lib/fixtures/overview.nothing_live.json';
import { headerTone, problemChip, statusText } from './status';

const o = (x: unknown) => structuredClone(x) as Overview;
const clean = () => ({ ...o(mining), problems: [] });
const withState = (state: Overview['status']['state']): Overview => {
	const x = o(nothingLive);
	x.status.state = state;
	return x;
};

beforeEach(() => setLang('en'));

describe('headerTone', () => {
	it('unreachable is red', () => {
		expect(headerTone(o(mining), true)).toBe('red');
		expect(headerTone(null, true)).toBe('red');
	});
	it('mining with a problem is amber', () => expect(headerTone(o(mining), false)).toBe('amber'));
	it('mining without problems is green', () => expect(headerTone(clean(), false)).toBe('green'));
	it('force watching is green, amber with a problem', () => {
		const x = withState('idle');
		x.force_watch = { live: [{ platform: 'kick', channel: 'xqc', watchers: ['a'] }], more: 0, total: 1 };
		x.problems = [];
		expect(headerTone(x, false)).toBe('green');
		x.problems = [{ kind: 'needs_login', account_id: 'b', account_name: 'b' }];
		expect(headerTone(x, false)).toBe('amber');
	});
	it('nothing_live is grey', () => {
		expect(headerTone(o(nothingLive), false)).toBe('grey');
		expect(headerTone(null, false)).toBe('grey');
	});
});

describe('statusText', () => {
	it('offline', () => expect(statusText(o(mining), true)).toBe('Miner offline'));
	it('not set up', () => {
		expect(statusText(o(firstRun), false)).toBe('Not set up');
		expect(statusText(withState('no_games'), false)).toBe('Not set up');
	});
	it('mining', () => expect(statusText(o(mining), false)).toBe('Mining · 2 of 3 accounts'));
	it('mining singular', () => {
		const x = o(mining);
		x.status.accounts_total = 1;
		x.status.accounts_mining = 1;
		expect(statusText(x, false)).toBe('Mining · 1 of 1 account');
	});
	it('nothing live', () => expect(statusText(o(nothingLive), false)).toBe('Idle · watching for campaigns'));
	it('idle', () => expect(statusText(withState('idle'), false)).toBe('Idle'));
	it('force watching when an account watches and none mines', () => {
		const x = withState('idle');
		x.force_watch = { live: [{ platform: 'twitch', channel: 'keshaeuw', watchers: ['Phluses'] }], more: 0, total: 1 };
		expect(statusText(x, false)).toBe('Force watching');
		x.force_watch.live[0].watchers = [];
		expect(statusText(x, false)).toBe('Idle');
		const m = o(mining);
		m.force_watch = { live: [{ platform: 'twitch', channel: 'keshaeuw', watchers: ['Phluses'] }], more: 0, total: 1 };
		expect(statusText(m, false)).toBe('Mining · 2 of 3 accounts');
	});
});

describe('problemChip', () => {
	it('one and many', () => {
		expect(problemChip(o(mining))).toEqual({ text: '1 account needs login', href: '/settings#accounts' });
		const two = o(mining);
		two.problems.push({ kind: 'needs_login', account_id: 'b', account_name: 'b' });
		expect(problemChip(two)).toEqual({ text: '2 accounts need login', href: '/settings#accounts' });
	});
	it('skips the account mid-login', () => {
		const x = o(mining);
		expect(problemChip(x, x.problems[0].account_id)).toBeNull();
		expect(problemChip(x, 'other')).not.toBeNull();
	});
	it('none gives null', () => {
		expect(problemChip(clean())).toBeNull();
		expect(problemChip(null)).toBeNull();
	});
});
