import { ACCOUNTS, INDEX, SECS, type Acct, type SecId } from './_data';

type Draft = 'sleepStart' | 'sleepEnd' | 'proxyUrl' | 'tgToken' | 'tgChat' | 'avatar' | 'mentionDiscord' | 'canaryKick';

const DRAFT_LABEL: Record<Draft, string> = {
	sleepStart: 'Sleep start',
	sleepEnd: 'Sleep end',
	proxyUrl: 'Proxy URL',
	tgToken: 'Telegram token',
	tgChat: 'Telegram chat',
	avatar: 'Avatar URL',
	mentionDiscord: 'Discord user ID',
	canaryKick: 'Kick canary channel'
};

/** One shared, in-memory copy of the settings. Every variant reads and writes it. */
class MockSettings {
	lang = $state('English');
	timefmt = $state<'24h' | '12h'>('24h');
	tz = $state('Server default (Europe/Madrid)');
	theme = $state<'dark' | 'light'>('dark');

	setTheme(t: 'dark' | 'light'): void {
		this.theme = t;
		document.documentElement.dataset.theme = t;
	}

	pm = $state<'ordered' | 'ending'>('ordered');
	preempt = $state(true);
	raids = $state(true);
	scan = $state(15);

	sleepOn = $state(true);
	sleepStart = $state('01:00');
	sleepEnd = $state('08:00');

	proxyOn = $state(false);
	proxyUrl = $state('');
	kickMode = $state<'auto' | 'browser' | 'ws'>('auto');

	share = $state(false);

	discord = $state(true);
	telegram = $state(false);
	tgToken = $state('');
	tgChat = $state('');
	events = $state({ claim: true, auth: true, errors: true, daily: false, codes: true, progress: false, canary: true });
	avatar = $state('');
	mentionDiscord = $state('');

	canaryEvery = $state('6 hours');
	canaryTwitch = $state('rocketleague');
	canaryKick = $state('xqc');

	autoCheck = $state(true);
	pwLogin = $state(true);
	sso = $state(true);

	accounts = $state<Acct[]>(ACCOUNTS.map((a) => ({ ...a })));

	/** Variant C collects typed edits here and saves them from a bar. Others save inline. */
	barMode = $state(false);
	dirty = $state<Partial<Record<Draft, string>>>({});
	saved = $state('');

	set(key: Draft, value: string): void {
		if (!(key in this.dirty)) this.dirty[key] = this[key];
		this[key] = value;
		if (this.dirty[key] === value) delete this.dirty[key];
	}
	get dirtyLabels(): string[] {
		return (Object.keys(this.dirty) as Draft[]).map((k) => DRAFT_LABEL[k]);
	}
	saveAll(): void {
		this.dirty = {};
		this.flash('Saved');
	}
	discard(): void {
		for (const [k, v] of Object.entries(this.dirty) as [Draft, string][]) this[k] = v;
		this.dirty = {};
	}
	/** Toggles and selects save at once; this is the quiet confirmation. */
	flash(msg = 'Saved'): void {
		this.saved = msg;
		setTimeout(() => {
			if (this.saved === msg) this.saved = '';
		}, 1600);
	}

	get counts(): { twitch: number; kick: number; bad: number } {
		return {
			twitch: this.accounts.filter((a) => a.platform === 'twitch').length,
			kick: this.accounts.filter((a) => a.platform === 'kick').length,
			bad: this.accounts.filter((a) => a.status === 'signed_out').length
		};
	}
	get eventsOn(): number {
		return Object.values(this.events).filter(Boolean).length;
	}

	/** One-line live summary per section, for nav rows and hub cards. */
	summary(id: SecId): { text: string; tone?: 'ok' | 'bad' | 'warn' | 'off' } {
		const c = this.counts;
		switch (id) {
			case 'accounts':
				return c.bad
					? { text: `${c.twitch} Twitch, ${c.kick} Kick · ${c.bad} needs login`, tone: 'warn' }
					: { text: `${c.twitch} Twitch, ${c.kick} Kick`, tone: 'ok' };
			case 'priority':
				return { text: `${this.pm === 'ordered' ? 'Priority order' : 'Ending soonest'} · every ${this.scan} min` };
			case 'sleep':
				return this.sleepOn ? { text: `${this.sleepStart} to ${this.sleepEnd}`, tone: 'ok' } : { text: 'Off', tone: 'off' };
			case 'network':
				return { text: `${this.proxyOn ? 'Proxy on' : 'No proxy'} · Kick ${this.kickMode === 'ws' ? 'WebSocket' : this.kickMode === 'auto' ? 'Auto' : 'Browser'}` };
			case 'experimental':
				return this.share ? { text: 'Shared catalog on', tone: 'ok' } : { text: 'Shared catalog off', tone: 'off' };
			case 'channels':
				return {
					text: [this.discord ? 'Discord on' : '', this.telegram ? 'Telegram on' : ''].filter(Boolean).join(' · ') || 'Not connected',
					tone: this.discord || this.telegram ? 'ok' : 'off'
				};
			case 'events':
				return { text: `${this.eventsOn} of 7 on` };
			case 'general':
				return { text: `${this.lang} · ${this.timefmt} · ${this.theme === 'light' ? 'Light' : 'Dark'}` };
			case 'health':
				return { text: 'Kick canary failing', tone: 'bad' };
			case 'updates':
				return { text: 'v2.2.2 · up to date', tone: 'ok' };
			case 'security':
				return { text: !this.sso ? 'Password only' : this.pwLogin ? 'Password or authentik' : 'authentik only' };
		}
	}
}

export const st = new MockSettings();

/** Scrolls to a setting row and flashes it. */
export function reveal(key: string): void {
	const el = document.getElementById(`set-${key}`);
	if (!el) return;
	const d = el.closest('details');
	if (d) d.open = true;
	el.scrollIntoView({ block: 'center' });
	el.classList.remove('flash');
	void el.offsetWidth;
	el.classList.add('flash');
}

export function search(q: string): { key: string; label: string; sec: SecId; secLabel: string }[] {
	const s = q.trim().toLowerCase();
	if (!s) return [];
	const name = (e: { label: string }) => (e.label.toLowerCase().includes(s) ? 0 : 1);
	return INDEX.filter((e) => `${e.label} ${e.words ?? ''} ${SECS[e.sec].label}`.toLowerCase().includes(s))
		.sort((a, b) => name(a) - name(b))
		.map((e) => ({ ...e, secLabel: SECS[e.sec].label }));
}
