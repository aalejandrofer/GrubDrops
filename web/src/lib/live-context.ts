import { getContext } from 'svelte';
import type { LiveStore } from '$lib/stores/live.svelte';

export const LIVE_KEY = 'live';

export function getLive(): LiveStore {
	return getContext<LiveStore>(LIVE_KEY);
}

export const NET_KEY = 'net';

/** Session boot reachability, set by the app layout while /api/session is Unreachable. */
export interface NetState {
	down: boolean;
	retryIn: number;
}

export function getNet(): NetState {
	return getContext<NetState>(NET_KEY);
}
