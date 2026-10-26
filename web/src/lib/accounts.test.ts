import { describe, expect, it } from 'vitest';
import type { Api } from '$lib/api/client';
import type { Account, DeviceStatus, Runner } from '$lib/api/types';
import accountsFx from '$lib/fixtures/accounts.json';
import runnersMining from '$lib/fixtures/runners.mining.json';
import { prefs } from '$lib/prefs.svelte';
import { MAX_POLL_FAILS, POLL_MS, accountLine, accountStatus, createDeviceFlow } from './accounts';

const ACC = (id: string) => structuredClone(accountsFx.find((a) => a.id === id)) as Account;
const RUN = (id: string) => structuredClone(runnersMining.find((r) => r.account_id === id)) as Runner;

function fake(statuses: DeviceStatus['status'][]) {
	const calls = { start: 0, status: 0, removed: [] as string[] };
	const api = {
		twitchStart: async () => {
			calls.start++;
			return { verification_url: 'https://www.twitch.tv/activate', user_code: 'ABCD-EFGH', expires_at: '' };
		},
		twitchStatus: async () => ({ status: statuses[Math.min(calls.status++, statuses.length - 1)] }),
		removeAccount: async (id: string) => {
			calls.removed.push(id);
		}
	} as unknown as Api;
	const delays: number[] = [];
	let pending: (() => void) | null = null;
	const schedule = (fn: () => void, ms: number) => {
		delays.push(ms);
		pending = fn;
		return () => {
			if (pending === fn) pending = null;
		};
	};
	const fire = async () => {
		const fn = pending;
		pending = null;
		fn?.();
		await new Promise((r) => setTimeout(r, 0));
	};
	return { api, calls, delays, schedule, fire, hasPending: () => pending !== null };
}

describe('device flow', () => {
	it('polls every 3 s until done', async () => {
		const f = fake(['pending', 'pending', 'done']);
		const flow = createDeviceFlow({ api: f.api, accountId: 'a9', schedule: f.schedule, isNew: true });
		await flow.start();
		expect(flow.state).toBe('code');
		expect(flow.code?.user_code).toBe('ABCD-EFGH');
		await f.fire();
		await f.fire();
		await f.fire();
		expect(f.calls.status).toBe(3);
		expect(f.delays).toEqual([POLL_MS, POLL_MS, POLL_MS]);
		expect(flow.state).toBe('done');
		expect(f.hasPending()).toBe(false);
	});

	it('expired offers a new code', async () => {
		const f = fake(['expired', 'pending']);
		const flow = createDeviceFlow({ api: f.api, accountId: 'a9', schedule: f.schedule, isNew: true });
		await flow.start();
		await f.fire();
		expect(flow.state).toBe('expired');
		expect(f.hasPending()).toBe(false);
		await flow.start();
		expect(f.calls.start).toBe(2);
		expect(flow.state).toBe('code');
	});

	it('an unknown status ends in error instead of a blank panel', async () => {
		const f = fake([undefined as unknown as DeviceStatus['status']]);
		const flow = createDeviceFlow({ api: f.api, accountId: 'a9', schedule: f.schedule, isNew: true });
		await flow.start();
		await f.fire();
		expect(flow.state).toBe('error');
		expect(f.hasPending()).toBe(false);
	});

	it('cancel on a new account deletes it', async () => {
		const f = fake(['pending']);
		const flow = createDeviceFlow({ api: f.api, accountId: 'a9', schedule: f.schedule, isNew: true });
		await flow.start();
		await flow.cancel();
		expect(f.calls.removed).toEqual(['a9']);
		expect(f.hasPending()).toBe(false);
	});

	it('cancel on an existing account keeps it', async () => {
		const f = fake(['pending']);
		const flow = createDeviceFlow({ api: f.api, accountId: 'a9', schedule: f.schedule, isNew: false });
		await flow.start();
		await flow.cancel();
		expect(f.calls.removed).toEqual([]);
		expect(f.hasPending()).toBe(false);
	});
});

describe('device flow races', () => {
	const mk = (f: ReturnType<typeof fake>) =>
		createDeviceFlow({ api: f.api, accountId: 'a9', schedule: f.schedule, isNew: false });

	it('start twice gives one polling chain', async () => {
		const f = fake(['pending']);
		const flow = mk(f);
		await Promise.all([flow.start(), flow.start()]);
		expect(f.delays).toEqual([POLL_MS]);
		await f.fire();
		expect(f.delays).toEqual([POLL_MS, POLL_MS]);
		expect(f.calls.status).toBe(1);
	});

	it('cancel during an in-flight status schedules nothing', async () => {
		const f = fake(['pending']);
		let release!: () => void;
		const gate = new Promise<void>((r) => (release = r));
		const real = f.api.twitchStatus;
		f.api.twitchStatus = async (id: string) => {
			await gate;
			return real(id);
		};
		const flow = mk(f);
		await flow.start();
		const fired = f.fire();
		await flow.cancel();
		release();
		await fired;
		expect(f.delays).toEqual([POLL_MS]);
		expect(f.hasPending()).toBe(false);
	});

	it('dispose during an in-flight start schedules nothing and keeps state', async () => {
		const f = fake(['pending']);
		let release!: () => void;
		const gate = new Promise<void>((r) => (release = r));
		const real = f.api.twitchStart;
		f.api.twitchStart = async (id: string) => {
			await gate;
			return real(id);
		};
		const flow = mk(f);
		const started = flow.start();
		flow.dispose();
		release();
		await started;
		expect(f.delays).toEqual([]);
		expect(flow.state).toBe('idle');
	});

	it('dispose clears a scheduled poll', async () => {
		const f = fake(['pending']);
		const flow = mk(f);
		await flow.start();
		flow.dispose();
		expect(f.hasPending()).toBe(false);
	});

	it('start during polling restarts with one chain', async () => {
		const f = fake(['pending']);
		const flow = mk(f);
		await flow.start();
		await flow.start();
		expect(f.calls.start).toBe(2);
		await f.fire();
		expect(f.calls.status).toBe(1);
		expect(f.delays).toEqual([POLL_MS, POLL_MS, POLL_MS]);
	});

	it('tolerates failed polls until MAX_POLL_FAILS in a row', async () => {
		const f = fake(['pending']);
		let n = 0;
		f.api.twitchStatus = async () => {
			n++;
			if (n === 3) return { status: 'pending' as const };
			throw new Error('net');
		};
		const flow = mk(f);
		await flow.start();
		await f.fire();
		await f.fire();
		expect(flow.state).toBe('code');
		await f.fire();
		expect(flow.state).toBe('code');
		n = 100;
		for (let i = 0; i < MAX_POLL_FAILS; i++) await f.fire();
		expect(flow.state).toBe('error');
		expect(f.hasPending()).toBe(false);
	});
});

describe('accountLine', () => {
	prefs.zone = 'UTC';
	prefs.fmt = '24h';
	it('mining', () => {
		expect(accountLine(ACC('acc-hjune'), RUN('acc-hjune'))).toBe(
			'Rust · Rust Reward 1 on seed_streamer · 42 / 60 min'
		);
	});
	it('needs login with the check time', () => {
		expect(accountLine(ACC('acc-kick'), undefined)).toBe('Session expired at 11:55. Mining stopped.');
	});
	it('needs login without a check', () => {
		const a = ACC('acc-kick');
		a.auth = null;
		expect(accountLine(a, undefined)).toBe('Not signed in.');
	});
	it('other states use the runner label', () => {
		const r = RUN('acc-hjune');
		r.state = 'sleeping';
		r.current = null;
		expect(accountLine(ACC('acc-hjune'), r)).toBe('Sleeping');
	});
});

describe('accountStatus', () => {
	it('maps to mining, signed out, idle', () => {
		expect(accountStatus(ACC('acc-hjune'), RUN('acc-hjune'))).toBe('mining');
		expect(accountStatus(ACC('acc-kick'), undefined)).toBe('signed_out');
		const r = RUN('acc-hjune');
		r.state = 'idle';
		r.current = null;
		expect(accountStatus(ACC('acc-hjune'), r)).toBe('idle');
		expect(accountStatus(ACC('acc-hjune'), undefined)).toBe('idle');
	});
});
