import type { Api } from '$lib/api/client';
import type { Account, DeviceStart, Runner } from '$lib/api/types';
import { clock } from '$lib/format';
import { t } from '$lib/i18n/index.svelte';
import { prefs } from '$lib/prefs.svelte';

export const POLL_MS = 3000;
/** Consecutive failed status polls before the flow gives up. */
export const MAX_POLL_FAILS = 3;

export type DeviceState = 'idle' | 'code' | 'done' | 'expired' | 'error';

export interface DeviceFlowDeps {
	api: Api;
	accountId: string;
	/** Run fn once after ms; returns a cancel function. */
	schedule: (fn: () => void, ms: number) => () => void;
	isNew: boolean;
}

export interface DeviceFlow {
	state: DeviceState;
	code: DeviceStart | null;
	start(): Promise<void>;
	cancel(): Promise<void>;
	/** Stops polling and ignores any request still in flight. Never touches the account. */
	dispose(): void;
	onChange(fn: () => void): void;
}

/** Twitch device-code login. Plain object: the component mirrors it into its own state from onChange. */
export function createDeviceFlow(deps: DeviceFlowDeps): DeviceFlow {
	const { api, accountId, schedule, isNew } = deps;
	let stopTimer: (() => void) | null = null;
	let listener: () => void = () => {};
	let gen = 0;
	let fails = 0;

	const flow: DeviceFlow = {
		state: 'idle',
		code: null,
		async start() {
			stopPolling();
			fails = 0;
			const mine = ++gen;
			try {
				const code = await api.twitchStart(accountId);
				if (mine !== gen) return;
				flow.code = code;
				flow.state = 'code';
				listener();
				arm(mine);
			} catch {
				if (mine !== gen) return;
				flow.state = 'error';
				listener();
			}
		},
		async cancel() {
			gen++;
			stopPolling();
			if (isNew) await api.removeAccount(accountId);
		},
		dispose() {
			gen++;
			stopPolling();
		},
		onChange(fn) {
			listener = fn;
		}
	};

	function stopPolling(): void {
		stopTimer?.();
		stopTimer = null;
	}

	function arm(mine: number): void {
		stopTimer = schedule(() => void poll(mine), POLL_MS);
	}

	async function poll(mine: number): Promise<void> {
		stopTimer = null;
		let status: Awaited<ReturnType<Api['twitchStatus']>>['status'] = 'pending';
		let failed = false;
		try {
			status = (await api.twitchStatus(accountId)).status;
		} catch {
			failed = true;
		}
		if (mine !== gen) return;
		if (failed) {
			fails++;
			if (fails < MAX_POLL_FAILS) {
				arm(mine);
				return;
			}
			status = 'error';
		} else {
			fails = 0;
		}
		if (status === 'pending') {
			arm(mine);
			return;
		}
		flow.state = status === 'done' || status === 'expired' ? status : 'error';
		listener();
	}

	return flow;
}

/** Twitch sign-in method: Android paste login or the TV device code. */
export type TwitchMethod = 'android' | 'tv';

/** True when the pasted text holds a token: the #access_token= redirect URL or a bare token. The server validates it. */
export function hasAndroidToken(text: string): boolean {
	const v = text.trim();
	return /(?:^|[#&?])access_token=\w+/.test(v) || /^\w{20,64}$/.test(v);
}

const ANDROID_ERRORS = new Map([
	['android_no_token', 'accounts.android_no_token'],
	['android_wrong_client', 'accounts.android_wrong_client'],
	['android_invalid_token', 'accounts.android_invalid_token']
]);

/** i18n key for an Android login error code from the API. */
export function androidErrorKey(code: string | undefined): string {
	return (code && ANDROID_ERRORS.get(code)) || 'accounts.android_failed';
}

export type AccountStatus ='paused' | 'mining' | 'signed_out' | 'idle';

/** Signed out wins, then paused: a paused account without a session still needs a login. */
export function accountStatus(a: Account, r: Runner | undefined): AccountStatus {
	if (a.needs_login || r?.state === 'needs_login') return 'signed_out';
	if (!a.enabled) return 'paused';
	if (r?.current && (r.state === 'watching' || r.state === 'claiming')) return 'mining';
	return 'idle';
}

/** Row buttons. A paused account without a session offers both: sign in, then resume. */
export function rowActions(a: Account, r: Runner | undefined): ('login' | 'resume')[] {
	const out: ('login' | 'resume')[] = [];
	if (a.needs_login || r?.state === 'needs_login') out.push('login');
	if (!a.enabled) out.push('resume');
	return out;
}

const STATE_KEYS = {
	idle: 'accounts.state.idle',
	picking: 'accounts.state.picking',
	starting: 'accounts.state.starting',
	watching: 'accounts.state.watching',
	claiming: 'accounts.state.claiming',
	sleeping: 'accounts.state.sleeping',
	awaiting_connect: 'accounts.state.awaiting_connect',
	needs_login: 'accounts.state.needs_login',
	no_games: 'accounts.state.no_games'
} as const;

export function accountLine(
	a: Account,
	r: Runner | undefined,
	time: (iso: string) => string = (iso) => clock(iso, prefs.zone, prefs.fmt)
): string {
	const status = accountStatus(a, r);
	if (status === 'paused') return t('accounts.paused_line');
	if (status === 'mining' && r?.current) {
		const c = r.current;
		return t('accounts.mining_line', {
			game: c.game,
			drop: c.benefit_name,
			channel: r.channel,
			m: c.minutes,
			req: c.required_minutes
		});
	}
	if (status === 'signed_out') {
		const at = a.auth?.checked_at;
		return at ? t('accounts.expired_at', { time: time(at) }) : t('accounts.not_signed_in');
	}
	return r ? t(STATE_KEYS[r.state]) : t('accounts.state.idle');
}

export function initials(name: string): string {
	const parts = name.split(/[\s_\-.]+/).filter(Boolean);
	const s = parts.length > 1 ? parts[0][0] + parts[1][0] : name.slice(0, 2);
	return s.toUpperCase();
}
