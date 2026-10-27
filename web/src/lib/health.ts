import type { HealthEvent } from '$lib/api/types';
import { t } from '$lib/i18n/index.svelte';

/** Exact English `msg` literals of frequent log lines, each with a localised `health.msg.*` key. */
export const MSG_KEYS: Record<string, string> = {
	'runner state change': 'health.msg.state_change',
	'runner integrity blocked; login needed': 'health.msg.login_needed',
	'runner picked drop': 'health.msg.picked_drop',
	'runner claim recorded': 'health.msg.claim_recorded',
	'runner reward claimed': 'health.msg.reward_claimed',
	'runner reward code captured': 'health.msg.code_captured',
	'runner claim failed': 'health.msg.claim_failed',
	'runner starting watch': 'health.msg.watch_start',
	'runner progress': 'health.msg.progress',
	'runner heartbeat sent': 'health.msg.heartbeat',
	'runner heartbeat failed': 'health.msg.heartbeat_failed',
	'runner channel went offline; swapping': 'health.msg.channel_offline',
	'runner benefit complete, claiming': 'health.msg.benefit_complete',
	'runner benefit frozen (minutes not advancing); rotating channel': 'health.msg.benefit_frozen',
	'runner benefit vanished from inventory; treating as externally claimed': 'health.msg.benefit_vanished',
	'twitch device-code authorized': 'health.msg.twitch_authorized',
	'twitch device-code expired before user authorized': 'health.msg.twitch_expired',
	'kick sidecar starting on demand': 'health.msg.sidecar_start',
	'kick sidecar ready': 'health.msg.sidecar_ready',
	'kick sidecar idle, stopping': 'health.msg.sidecar_stop',
	'canary: probe complete': 'health.msg.canary_done',
	'canary run-now failed': 'health.msg.canary_failed'
};

const KIND_KEYS: Record<string, string> = {
	claim: 'health.kind.claim',
	progress: 'health.kind.progress',
	state: 'health.kind.state',
	discovery: 'health.kind.discovery',
	error: 'health.kind.error',
	auth: 'health.kind.auth',
	info: 'health.kind.info',
	heartbeat: 'health.kind.heartbeat'
};

const STATES = new Set([
	'idle',
	'picking',
	'starting',
	'watching',
	'claiming',
	'sleeping',
	'awaiting_connect',
	'needs_login',
	'no_games',
	'force_watching'
]);

/** Runner state enums read through the accounts.state.* labels; anything else stays raw. */
function localiseFields(f: Record<string, string>): Record<string, string> {
	const out = { ...f };
	for (const k of ['state', 'prev']) {
		const v = out[k];
		if (v !== undefined && STATES.has(v)) out[k] = t(`accounts.state.${v}`);
	}
	return out;
}

export function kindLabel(kind: string): string {
	const key = Object.hasOwn(KIND_KEYS, kind) ? KIND_KEYS[kind] : undefined;
	return key ? t(key) : kind;
}

export function isProblem(e: HealthEvent): boolean {
	return e.level === 'WARN' || e.level === 'ERROR' || e.kind === 'error' || e.kind === 'auth';
}

/** Localised text of a log line, or the raw message when unmapped or a placeholder has no value. */
export function eventText(e: HealthEvent): string {
	const f = e.fields ?? {};
	const key = Object.hasOwn(MSG_KEYS, e.msg) ? MSG_KEYS[e.msg] : undefined;
	let text = e.msg;
	let hasErr = false;
	if (key) {
		const names = [...t(key).matchAll(/\{(\w+)\}/g)].map((m) => m[1]);
		if (names.every((n) => n in f)) {
			text = t(key, localiseFields(f));
			hasErr = names.includes('err');
		}
	}
	return f.err && !hasErr ? `${text} · ${f.err}` : text;
}
