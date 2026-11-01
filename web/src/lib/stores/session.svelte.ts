import type { Api } from '$lib/api/client';

export interface SessionStore {
	loaded: boolean;
	authed: boolean;
	setupNeeded: boolean;
	oidc: { enabled: boolean; name: string };
	passwordLogin: boolean;
	csrf: string;
	version: string;
	load(): Promise<void>;
}

export function createSession(api: Api): SessionStore {
	const s: SessionStore = $state({
		loaded: false,
		authed: false,
		setupNeeded: false,
		oidc: { enabled: false, name: '' },
		passwordLogin: true,
		csrf: '',
		version: '',
		load: async () => {
			const r = await api.session();
			s.authed = r.authed;
			s.setupNeeded = r.setup_needed;
			s.oidc = r.oidc;
			s.passwordLogin = r.password_login !== false;
			s.csrf = r.csrf_token;
			s.version = r.version;
			s.loaded = true;
		}
	});
	return s;
}
