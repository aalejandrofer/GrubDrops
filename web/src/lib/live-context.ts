import { getContext } from 'svelte';
import type { LiveStore } from '$lib/stores/live.svelte';

export const LIVE_KEY = 'live';

export function getLive(): LiveStore {
	return getContext<LiveStore>(LIVE_KEY);
}
