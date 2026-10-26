import { goto } from '$app/navigation';
import { createClient } from '$lib/api/client';
import { createSession, type SessionStore } from '$lib/stores/session.svelte';

// session needs api to load and api needs session for the token, so bind late.
let sessionRef: SessionStore | undefined;

export const api = createClient({
	csrf: () => sessionRef?.csrf ?? '',
	refreshCsrf: () => sessionRef!.load(),
	onUnauthorized: () => {
		const p = typeof window === 'undefined' ? '' : window.location.pathname;
		if (p === '/login' || p === '/setup') return;
		void goto('/login');
	},
	onUnreachable: () => {}
});

export const session = createSession(api);
sessionRef = session;
