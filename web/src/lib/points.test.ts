import { describe, expect, it } from 'vitest';
import type { Account, ForceChannel, Platform, PointsResp, Runner } from '$lib/api/types';
import points from './fixtures/points.json';
import {
	BAL_KEYS,
	accountFresh,
	addChannel,
	balanceIndex,
	balanceOf,
	balanceView,
	cardState,
	chanKey,
	fwNote,
	moveInTab,
	pointsTickMs,
	pointsTotal,
	removeChannel,
	tabRows,
	toggleAccount,
	watchers,
	watchStatus
} from './points';

const resp = points as PointsResp;

const ch = (channel: string, platform: Platform = 'twitch', accounts: string[] = [], live: boolean | null = null): ForceChannel => ({
	platform,
	channel,
	live,
	accounts,
	watching: []
});

const list = [
	ch('keshaeuw', 'twitch', ['t1', 't2', 't3'], true),
	ch('xqc', 'kick', ['k1'], true),
	ch('caedrel', 'twitch', ['t1', 't2'], false),
	ch('tarik', 'twitch', ['t2'], false)
];

const acct = (over: Partial<Account> = {}): Account =>
	({ id: 't1', platform: 'twitch', name: 'TTik3r', enabled: true, needs_login: false, points_mode: 'drops_points', ...over }) as Account;

const runner = (over: Partial<Runner>): Runner =>
	({ account_id: 't1', platform: 'twitch', state: 'idle', current: null, channel: '', ...over }) as Runner;

describe('ordering', () => {
	it('tabRows keeps one platform in rank order', () => {
		expect(tabRows(list, 'twitch').map((c) => c.channel)).toEqual(['keshaeuw', 'caedrel', 'tarik']);
		expect(tabRows(list, 'kick').map((c) => c.channel)).toEqual(['xqc']);
	});
	it('moveInTab reorders the tab and leaves the other platform in its slot', () => {
		const out = moveInTab(list, 'twitch', 2, 0);
		expect(out.map(chanKey)).toEqual(['twitch/tarik', 'kick/xqc', 'twitch/keshaeuw', 'twitch/caedrel']);
		expect(list.map((c) => c.channel)).toEqual(['keshaeuw', 'xqc', 'caedrel', 'tarik']);
	});
	it('moveInTab ignores out-of-range moves', () => {
		expect(moveInTab(list, 'twitch', 0, 5).map(chanKey)).toEqual(list.map(chanKey));
		expect(moveInTab(list, 'kick', 0, 0).map(chanKey)).toEqual(list.map(chanKey));
	});
	it('toggleAccount ticks and unticks one channel only', () => {
		const off = toggleAccount(list, 'twitch/caedrel', 't1');
		expect(off[2].accounts).toEqual(['t2']);
		expect(off[0].accounts).toEqual(['t1', 't2', 't3']);
		expect(toggleAccount(off, 'twitch/caedrel', 't1')[2].accounts).toEqual(['t2', 't1']);
	});
	it('removeChannel drops by key', () => {
		expect(removeChannel(list, 'kick/xqc').map(chanKey)).toEqual(['twitch/keshaeuw', 'twitch/caedrel', 'twitch/tarik']);
	});
});

describe('addChannel', () => {
	it('adds at the bottom with no accounts', () => {
		const r = addChannel(list, 'https://www.twitch.tv/Shroud', 'twitch');
		expect(r).toEqual({ list: [...list, ch('shroud')], key: 'twitch/shroud' });
	});
	it('rejects bad links, duplicates and the other tab', () => {
		expect(addChannel(list, 'youtube.com/x', 'twitch')).toEqual({ err: 'bad' });
		expect(addChannel(list, 'twitch.tv/caedrel', 'twitch')).toEqual({ err: 'dupe' });
		expect(addChannel(list, 'kick.com/adinross', 'twitch')).toEqual({ err: 'other_tab', platform: 'kick' });
	});
});

describe('balances', () => {
	const idx = balanceIndex(resp);

	it('balanceIndex finds a balance by channel and account', () => {
		expect(balanceOf(idx, ch('caedrel'), 't2')?.balance).toBe(9875);
		expect(balanceOf(idx, ch('caedrel'), 't3')).toBeUndefined();
		expect(balanceIndex(null).size).toBe(0);
	});
	it('balanceView maps every status', () => {
		expect(balanceView(balanceOf(idx, ch('keshaeuw'), 't2'))).toEqual({ value: 48210, tone: 'ok', at: '2026-10-31T09:57:00Z' });
		expect(balanceView(balanceOf(idx, ch('keshaeuw'), 't1'))).toEqual({ value: 12450, tone: 'error', at: '2026-10-31T08:00:00Z' });
		expect(balanceView(balanceOf(idx, ch('keshaeuw'), 't3'))?.tone).toBe('off');
		expect(balanceView(balanceOf(idx, ch('tarik'), 't2'))).toEqual({ value: null, tone: 'pending', at: null });
		expect(balanceView(balanceOf(idx, ch('xqc', 'kick'), 'k1'))).toEqual({ value: null, tone: 'na', at: null });
		expect(balanceView(undefined)).toBeNull();
		const stale = { ...balanceOf(idx, ch('caedrel'), 't1')!, stale: true };
		expect(balanceView(stale)?.tone).toBe('stale');
	});
	it('every tone has a tooltip key', () => {
		expect(Object.values(BAL_KEYS).every((k) => k.startsWith('points.bal_'))).toBe(true);
	});
});

describe('accountFresh', () => {
	it('a failed read names the age of the value it shows', () => {
		expect(accountFresh(resp, 't1')).toEqual({ kind: 'error', at: '2026-10-31T08:00:00Z' });
	});
	it('else the newest read, stale when any value is', () => {
		expect(accountFresh(resp, 't2')).toEqual({ kind: 'ok', at: '2026-10-31T09:57:00Z', stale: false });
		expect(accountFresh(resp, 't3')).toEqual({ kind: 'ok', at: '2026-10-28T10:00:00Z', stale: true });
	});
	it('pending before the first read, none without balances', () => {
		const only: PointsResp = { ...resp, channels: resp.channels.filter((c) => c.channel === 'tarik') };
		expect(accountFresh(only, 't2')).toEqual({ kind: 'pending' });
		expect(accountFresh(resp, 'k1')).toEqual({ kind: 'none' });
		expect(accountFresh(null, 't1')).toEqual({ kind: 'none' });
	});
});

describe('fwNote', () => {
	it('explains why Force watch runs or not', () => {
		expect(fwNote(acct({ enabled: false }), undefined)).toBe('points.fw_paused');
		expect(fwNote(acct({ points_mode: 'off' }), undefined)).toBe('points.fw_points_off');
		expect(fwNote(acct({ points_mode: 'points_only' }), undefined)).toBe('points.fw_always');
		const mining = runner({ state: 'watching', current: {} as Runner['current'] });
		expect(fwNote(acct(), mining)).toBe('points.fw_waits');
		expect(fwNote(acct(), runner({ state: 'force_watching' }))).toBe('points.fw_on');
	});
	it('Kick accounts have no points mode and always Force watch', () => {
		expect(fwNote(acct({ platform: 'kick', points_mode: 'off' }), undefined)).toBe('points.fw_on');
	});
});

describe('watchers', () => {
	it('lists who watches each live channel, in rank order', () => {
		const names = new Map([
			['t2', 'Phluses'],
			['k1', 'TTik3r']
		]);
		const rs = [
			runner({ account_id: 'k1', platform: 'kick', state: 'force_watching', channel: 'xqc' }),
			runner({ account_id: 't2', state: 'force_watching', channel: 'KeshaEUW' })
		];
		expect(watchers(list, rs, names)).toEqual([
			{ name: 'Phluses', channel: 'keshaeuw' },
			{ name: 'TTik3r', channel: 'xqc' }
		]);
		expect(watchers(list.map((c) => ({ ...c, live: false })), rs, names)).toEqual([]);
	});
});

describe('pointsTickMs', () => {
	const now = Date.parse('2026-11-01T12:00:00Z');
	const resp = (at: string | null): PointsResp => ({
		refresh_every_sec: 3600,
		accounts: [],
		channels: [
			{
				platform: 'twitch',
				channel: 'x',
				live: true,
				balances: [{ account_id: 'a', status: 'ok', balance: 1, fetched_at: at, checked_at: at, stale: false }]
			}
		]
	});
	it('ticks every 5s while a balance time is under a minute old', () => {
		expect(pointsTickMs(resp('2026-11-01T11:59:30Z'), now)).toBe(5_000);
	});
	it('ticks every 30s once every time is older, or there is none', () => {
		expect(pointsTickMs(resp('2026-11-01T11:58:00Z'), now)).toBe(30_000);
		expect(pointsTickMs(resp(null), now)).toBe(30_000);
		expect(pointsTickMs(null, now)).toBe(30_000);
	});
});

describe('watchStatus', () => {
	const w = (name: string, channel: string) => ({ name, channel });
	it('is undefined with nobody watching', () => {
		expect(watchStatus([])).toBeUndefined();
	});
	it('names one or two watchers and their channels', () => {
		expect(watchStatus([w('seed_twitch', 'shroud')])).toBe('Points watching · seed_twitch on shroud');
		expect(watchStatus([w('seed_twitch', 'shroud'), w('grub_alt', 'xqc')])).toBe(
			'Points watching · seed_twitch on shroud, grub_alt on xqc'
		);
	});
	it('collapses to a count past two', () => {
		expect(watchStatus([w('a', 'x'), w('b', 'y'), w('c', 'z')])).toBe('Points watching · 3 accounts');
	});
});

describe('pointsTotal', () => {
	const p = {
		refresh_every_sec: 3600,
		channels: [],
		accounts: [
			{ id: 't1', name: 'a', platform: 'twitch', enabled: true, points_mode: 'drops_points', balances_available: true, total: 48210 },
			{ id: 't2', name: 'b', platform: 'twitch', enabled: true, points_mode: 'drops_points', balances_available: true, total: null },
			{ id: 'k1', name: 'c', platform: 'kick', enabled: true, points_mode: 'off', balances_available: false, total: 0 }
		]
	} as PointsResp;
	it('gives the total when balances exist', () => {
		expect(pointsTotal(p, 't1')).toBe(48210);
	});
	it('omits it with no known total, on Kick, or for an unknown account', () => {
		expect(pointsTotal(p, 't2')).toBeNull();
		expect(pointsTotal(p, 'k1')).toBeNull();
		expect(pointsTotal(p, 'nope')).toBeNull();
		expect(pointsTotal(null, 't1')).toBeNull();
	});
});

describe('cardState', () => {
	const now = Date.parse('2026-11-01T12:00:00Z');
	const started = '2026-11-01T11:23:00Z';
	it('shows channel and time while force watching', () => {
		const st = cardState(acct(), runner({ state: 'force_watching', channel: 'shroud', started_at: started }), now);
		expect(st.on).toBe(true);
		expect(st.text).toBe('Watching shroud · 37m');
		expect(st.parts).toEqual([
			{ text: 'Watching ', kind: 'text' },
			{ text: 'shroud', kind: 'channel' },
			{ text: ' · ', kind: 'text' },
			{ text: '37m', kind: 'time' }
		]);
	});
	it('drops the time once force watching ends, though started_at stays', () => {
		const st = cardState(acct(), runner({ state: 'sleeping', channel: 'shroud', started_at: started }), now);
		expect(st.on).toBe(false);
		expect(st.text).not.toContain('37m');
		expect(st.parts).toEqual([{ text: st.text, kind: 'text' }]);
	});
	it('keeps the account line for other states', () => {
		const mining = runner({
			state: 'watching',
			current: { game: 'Rust' } as Runner['current'],
			started_at: started
		});
		expect(cardState(acct(), mining, now)).toMatchObject({ on: false, text: 'Mining: Rust' });
		expect(cardState(acct({ enabled: false }), undefined, now)).toMatchObject({ on: false, text: 'Paused' });
	});
	it('falls back to the account line without a start time', () => {
		const st = cardState(acct(), runner({ state: 'force_watching', channel: 'shroud', started_at: null }), now);
		expect(st).toMatchObject({ on: false, text: 'Points watching shroud' });
	});
	it('counts a paused account as not watching', () => {
		const st = cardState(acct({ enabled: false }), runner({ state: 'force_watching', channel: 'shroud', started_at: started }), now);
		expect(st.on).toBe(false);
	});
});
