import type { Api } from '$lib/api/client';
import type { Account, Claim, ForceChannel, GamesResp, HealthEvent, Overview, Runner } from '$lib/api/types';

export interface EventSourceLike {
	onopen: (() => void) | null;
	onerror: (() => void) | null;
	addEventListener(name: string, fn: (ev: { data: string }) => void): void;
	close(): void;
}

export interface LiveDeps {
	api: Api;
	eventSource: (url: string) => EventSourceLike;
	/** Run fn once after ms; returns a cancel function. */
	schedule?: (fn: () => void, ms: number) => () => void;
	/** Run fn every ms; returns a cancel function. Drives the retryIn countdown. */
	tick?: (fn: () => void, ms: number) => () => void;
	now?: () => number;
	onUnauthorized: () => void;
}

export interface LiveStore {
	overview: Overview | null;
	runners: Runner[];
	games: GamesResp | null;
	accounts: Account[];
	recent: Claim[];
	log: HealthEvent[];
	/** Force watch channels in rank order, or null before they load. */
	forceWatch: ForceChannel[] | null;
	/** The last overview or games load failed. Cleared by the next success. */
	failed: { overview: boolean; games: boolean };
	unreachable: boolean;
	retryIn: number;
	updatedAt: number | null;
	start(): void;
	stop(): void;
	refetchAll(): Promise<void>;
	seedLog(events: HealthEvent[]): void;
}

export const BACKOFF = [1, 2, 4, 8, 16, 30];
const MAX_RECENT = 10;
const MAX_LOG = 50;

/** Replace the item with the same key in place, else prepend it. */
export function upsertBy<T>(list: T[], item: T, key: (x: T) => string): T[] {
	const k = key(item);
	const i = list.findIndex((x) => key(x) === k);
	if (i < 0) return [item, ...list];
	const next = list.slice();
	next[i] = item;
	return next;
}

const defaultSchedule = (fn: () => void, ms: number) => {
	const id = setTimeout(fn, ms);
	return () => clearTimeout(id);
};
const defaultTick = (fn: () => void, ms: number) => {
	const id = setInterval(fn, ms);
	return () => clearInterval(id);
};

export function createLive(deps: LiveDeps): LiveStore {
	const { api } = deps;
	const schedule = deps.schedule ?? defaultSchedule;
	const tick = deps.tick ?? defaultTick;
	const now = deps.now ?? Date.now;

	let source: EventSourceLike | null = null;
	let cancelReconnect: (() => void) | null = null;
	let cancelTick: (() => void) | null = null;
	let backoffIndex = 0;
	let dead = false;

	const s: LiveStore = $state({
		overview: null,
		runners: [],
		games: null,
		accounts: [],
		recent: [],
		log: [],
		forceWatch: null,
		failed: { overview: false, games: false },
		unreachable: false,
		retryIn: 0,
		updatedAt: null,
		start,
		stop,
		refetchAll,
		seedLog
	});

	const touch = () => {
		s.updatedAt = now();
	};

	async function quiet(p: Promise<void>): Promise<void> {
		try {
			await p;
		} catch {
			// the client reports unreachable and 401 itself
		}
	}

	const loadOverview = async () => {
		try {
			s.overview = await api.overview();
			s.failed.overview = false;
		} catch (e) {
			s.failed.overview = true;
			throw e;
		}
	};
	const loadRunners = async () => {
		s.runners = await api.runners();
	};
	const loadGames = async () => {
		try {
			s.games = await api.games();
			s.failed.games = false;
		} catch (e) {
			s.failed.games = true;
			throw e;
		}
	};
	const loadAccounts = async () => {
		s.accounts = await api.accounts();
	};
	const loadForceWatch = async () => {
		s.forceWatch = (await api.forceWatch()).channels;
	};
	const loadRecent = async () => {
		s.recent = (await api.claims({ limit: MAX_RECENT })).items.slice(0, MAX_RECENT);
	};

	async function refetchAll(): Promise<void> {
		await Promise.all(
			[loadOverview, loadRunners, loadGames, loadAccounts, loadRecent, loadForceWatch].map((f) => quiet(f()))
		);
		touch();
	}

	function seedLog(events: HealthEvent[]): void {
		s.log = events.slice(0, MAX_LOG);
	}

	function parse<T>(ev: { data: string }): T | null {
		try {
			return JSON.parse(ev.data) as T;
		} catch {
			return null;
		}
	}

	function clearTimers(): void {
		cancelReconnect?.();
		cancelReconnect = null;
		cancelTick?.();
		cancelTick = null;
	}

	function connect(): void {
		cancelReconnect = null;
		const src = deps.eventSource('/api/events');
		source = src;
		src.onopen = () => {
			if (dead || source !== src) return;
			backoffIndex = 0;
			clearTimers();
			s.unreachable = false;
			s.retryIn = 0;
			void refetchAll();
		};
		src.onerror = () => void onError(src);
		src.addEventListener('runner', (ev) => {
			const r = parse<Runner>(ev);
			if (!r) return;
			const i = s.runners.findIndex((x) => x.account_id === r.account_id);
			const was = i < 0 ? undefined : s.runners[i].state;
			s.runners = i < 0 ? [...s.runners, r] : s.runners.map((x, j) => (j === i ? r : x));
			touch();
			// The Home force watch tile names who watches; it lives on the overview.
			const force = (st: string | undefined) => st === 'force_watching';
			// The force list carries live state; a switch is the cheap moment to refresh it.
			if (force(was) !== force(r.state)) {
				void quiet(loadOverview());
				void quiet(loadForceWatch());
			}
		});
		src.addEventListener('claim', (ev) => {
			const c = parse<Claim>(ev);
			if (!c) return;
			s.recent = upsertBy(s.recent, c, (x) => `${x.account_id}:${x.benefit_id}`).slice(0, MAX_RECENT);
			touch();
			void quiet(loadOverview());
		});
		src.addEventListener('games', () => {
			touch();
			void quiet(loadGames());
			void quiet(loadOverview());
		});
		src.addEventListener('accounts', () => {
			touch();
			void quiet(loadAccounts());
			void quiet(loadRunners());
			void quiet(loadOverview());
			// A removed account drops out of its force watch channels.
			void quiet(loadForceWatch());
		});
		src.addEventListener('force_watch', () => {
			touch();
			void quiet(loadForceWatch());
			void quiet(loadOverview());
		});
		src.addEventListener('log', (ev) => {
			const e = parse<HealthEvent>(ev);
			if (!e) return;
			s.log = [e, ...s.log].slice(0, MAX_LOG);
			touch();
		});
	}

	async function onError(src: EventSourceLike): Promise<void> {
		if (dead || source !== src) return;
		src.close();
		source = null;
		clearTimers();
		let authed = true;
		try {
			authed = (await api.session()).authed;
		} catch (e) {
			if ((e as { status?: number }).status === 401) authed = false;
		}
		if (dead || source !== null) return;
		if (!authed) {
			dead = true;
			deps.onUnauthorized();
			return;
		}
		const delay = BACKOFF[Math.min(backoffIndex, BACKOFF.length - 1)];
		backoffIndex++;
		s.unreachable = true;
		s.retryIn = delay;
		cancelTick = tick(() => {
			if (s.retryIn > 0) s.retryIn -= 1;
		}, 1000);
		cancelReconnect = schedule(() => {
			cancelTick?.();
			cancelTick = null;
			if (!dead) connect();
		}, delay * 1000);
	}

	function start(): void {
		dead = false;
		stop();
		dead = false;
		backoffIndex = 0;
		connect();
	}

	function stop(): void {
		dead = true;
		clearTimers();
		source?.close();
		source = null;
	}

	return s;
}
