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
	'canary run-now failed': 'health.msg.canary_failed',
	authcheck: 'health.msg.authcheck',
	'authcheck: avatar updated': 'health.msg.avatar_updated',
	'kick ws watch started': 'health.msg.watch_start',
	'kick ws watch reconnecting': 'health.msg.ws_reconnect',
	'runner reward reaper: nothing to claim': 'health.msg.nothing_to_claim',
	'runner inventory sweep done': 'health.msg.sweep_done',
	'runner benefit already claimed in inventory': 'health.msg.already_claimed',
	'runner ready drop claim failed': 'health.msg.claim_failed',
	'runner claimed drop after account link': 'health.msg.claimed_linked',
	'runner benefit never appeared in inventory (claimed or ghost); skipping': 'health.msg.never_appeared',
	'runner force watching': 'health.msg.force_watch',
	'runner joined raid': 'health.msg.raid',
	'runner claimed channel points bonus': 'health.msg.points_bonus',
	'runner step recovered': 'health.msg.recovered',
	'runner step error; retrying after backoff': 'health.msg.step_error',
	'runner preempting drop for a higher-ranked campaign': 'health.msg.preempt',
	'runner switching drop on request': 'health.msg.switch_request',
	'runner mine now requested': 'health.msg.mine_now',
	'runner drop marked collected; moving on': 'health.msg.marked_collected',
	'runner sleep window; stopping until wake': 'health.msg.sleep_start',
	'runner sleep window over': 'health.msg.sleep_end',
	'runner no eligible streams live in any queued campaign; sleeping': 'health.msg.no_streams',
	'runner no eligible streams live, trying next queued campaign': 'health.msg.no_streams_next',
	'session refreshed': 'health.msg.session_refreshed',
	'session refresh failed, will idle': 'health.msg.session_refresh_failed',
	'account has no session, will idle until re-auth': 'health.msg.no_session',
	'discover: fetch failed': 'health.msg.scan_failed',
	'miner starting': 'health.msg.miner_start',
	'pubsub connected': 'health.msg.pubsub_up',
	'pubsub disconnected': 'health.msg.pubsub_down',
	'http listening': 'health.msg.http_up',
	'twitch backend: direct HTTP (Android device-code, no integrity header)': 'health.msg.twitch_ready',
	'kick backend enabled (utls HTTP transport)': 'health.msg.kick_ready',
	'kick sidecar auto-create enabled': 'health.msg.sidecar_auto',
	'docker control enabled for on-demand kick sidecars': 'health.msg.docker_on',
	'docker control unavailable; kick sidecars stay always-on': 'health.msg.docker_off',
	'kick: auto watch requested but no sidecar client configured; running pure WebSocket with no Chrome fallback': 'health.msg.ws_only'
};

/** Chip colour of a log line. */
export type Tone = 'ok' | 'bad' | 'warn' | 'info' | 'off' | 'violet';

/** Known event kinds: chip label key and tone. */
const KINDS: Record<string, { key: string; tone: Tone }> = {
	claim: { key: 'health.kind.claim', tone: 'ok' },
	progress: { key: 'health.kind.progress', tone: 'info' },
	watch: { key: 'health.kind.watch', tone: 'info' },
	state: { key: 'health.kind.state', tone: 'off' },
	discovery: { key: 'health.kind.discovery', tone: 'violet' },
	error: { key: 'health.kind.error', tone: 'bad' },
	auth: { key: 'health.kind.auth', tone: 'warn' },
	info: { key: 'health.kind.info', tone: 'off' },
	heartbeat: { key: 'health.kind.heartbeat', tone: 'off' }
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
	'force_watching',
	'sleep_window',
	'no_channels'
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

const kindOf = (kind: string) => (Object.hasOwn(KINDS, kind) ? KINDS[kind] : undefined);

export function kindLabel(kind: string): string {
	const k = kindOf(kind);
	return k ? t(k.key) : kind;
}

/**
 * Chip label and tone of a log line. ERROR lines read red and WARN lines amber whatever their kind,
 * so a failed claim never shows a green Claim chip. An unknown kind keeps its raw name on a neutral chip;
 * a line with no kind is labelled by its level.
 */
export function kindChip(e: HealthEvent): { label: string; tone: Tone } {
	const k = kindOf(e.kind);
	const label = k
		? t(k.key)
		: e.kind || t(e.level === 'ERROR' ? 'health.kind.error' : e.level === 'WARN' ? 'health.kind.warning' : 'health.kind.info');
	let tone: Tone = k?.tone ?? 'off';
	// A sign-in check that carries its result reads green or red.
	const ok = authOk(e);
	if (ok !== undefined) tone = ok ? 'ok' : 'bad';
	if (e.level === 'ERROR') tone = 'bad';
	else if (e.level === 'WARN' && tone !== 'bad') tone = 'warn';
	return { label, tone };
}

export type Filter = 'all' | 'problems' | 'claims';
export type Counts = Record<Filter, number>;

/** True when a log line belongs under the filter. */
export function matchesFilter(f: Filter, e: HealthEvent): boolean {
	return f === 'all' || (f === 'problems' ? isProblem(e) : e.kind === 'claim');
}

/** Filter counts of a list of lines: the same lines the filters list, so each count matches its list. */
export function countFilters(events: HealthEvent[]): Counts {
	const c: Counts = { all: 0, problems: 0, claims: 0 };
	for (const e of events) {
		if (e.level === 'DEBUG') continue;
		c.all++;
		if (matchesFilter('problems', e)) c.problems++;
		if (matchesFilter('claims', e)) c.claims++;
	}
	return c;
}

/** Pass or fail of an auth line that carries an ok field, else undefined. */
export function authOk(e: HealthEvent): boolean | undefined {
	const ok = e.kind === 'auth' ? e.fields?.ok : undefined;
	return ok === 'true' ? true : ok === 'false' ? false : undefined;
}

/** A failure: WARN or ERROR, an error line, or an auth line whose ok field is false. A passing sign-in check is not one. */
export function isProblem(e: HealthEvent): boolean {
	return e.level === 'WARN' || e.level === 'ERROR' || e.kind === 'error' || authOk(e) === false;
}

/** Localised text of a log line, or the raw message when unmapped or a placeholder has no value. */
export function eventText(e: HealthEvent): string {
	const f = e.fields ?? {};
	const ok = e.msg === 'authcheck' ? authOk(e) : undefined;
	if (ok !== undefined) return t(ok ? 'health.msg.authcheck_ok' : 'health.msg.authcheck_failed');
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
