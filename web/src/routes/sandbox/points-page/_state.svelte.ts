import type { Platform, PointsMode } from '$lib/api/types';
import { accounts, balances, channels, parseChannel, type Acct, type Chan } from './_data';

/** Mock page state: a fresh copy per mockup, nothing saved anywhere. */
export class PointsState {
	tab = $state<Platform>('twitch');
	accts = $state<Acct[]>(structuredClone(accounts));
	lists = $state<Record<Platform, Chan[]>>({
		twitch: structuredClone(channels.filter((c) => c.platform === 'twitch')),
		kick: structuredClone(channels.filter((c) => c.platform === 'kick'))
	});
	bal = $state<Record<string, Record<string, number>>>(structuredClone(balances));
	/** Row being dragged and where it would land. */
	drag = $state<{ id: string; to: number } | null>(null);
	moved = $state<string | null>(null);
	refreshing = $state(false);

	/** The tab's ordered list, with a running drag previewed. */
	get rows(): Chan[] {
		const list = this.lists[this.tab];
		const d = this.drag;
		if (!d) return list;
		const from = list.findIndex((c) => c.id === d.id);
		if (from < 0 || from === d.to) return list;
		const next = [...list];
		const [c] = next.splice(from, 1);
		next.splice(d.to, 0, c);
		return next;
	}

	get tabAccts(): Acct[] {
		return this.accts.filter((a) => a.platform === this.tab);
	}

	name(id: string): string {
		return this.accts.find((a) => a.id === id)?.name ?? id;
	}

	acct(id: string): Acct | undefined {
		return this.accts.find((a) => a.id === id);
	}

	balance(chan: string, acct: string): number | null {
		return this.bal[chan]?.[acct] ?? null;
	}

	/** Sum over the account's balances on channels in the list. */
	total(acct: string): number {
		return this.lists.twitch.reduce((n, c) => n + (this.bal[c.id]?.[acct] ?? 0), 0);
	}

	toggle(chan: string, acct: string): void {
		const c = this.lists[this.tab].find((x) => x.id === chan);
		if (!c) return;
		c.accounts = c.accounts.includes(acct) ? c.accounts.filter((x) => x !== acct) : [...c.accounts, acct];
	}

	setMode(acct: string, mode: PointsMode): void {
		const a = this.acct(acct);
		if (a) a.mode = mode;
	}

	move(from: number, to: number): void {
		const list = this.lists[this.tab];
		if (to < 0 || to >= list.length || from === to) return;
		const next = [...list];
		const [c] = next.splice(from, 1);
		next.splice(to, 0, c);
		this.lists[this.tab] = next;
		this.flash(c.id);
	}

	remove(chan: string): void {
		this.lists[this.tab] = this.lists[this.tab].filter((c) => c.id !== chan);
	}

	/** Returns an error message, or '' once added. */
	add(raw: string): string {
		const p = parseChannel(raw);
		if (!p) return 'Paste a twitch.tv or kick.com channel link.';
		if (p.platform !== this.tab) return `That's a ${p.platform === 'twitch' ? 'Twitch' : 'Kick'} link. Switch tabs to add it.`;
		if (this.lists[p.platform].some((c) => c.name === p.name)) return 'Already in the list.';
		const id = `n-${p.platform}-${p.name}`;
		this.lists[p.platform] = [...this.lists[p.platform], { id, platform: p.platform, name: p.name, live: false, accounts: [], watching: [] }];
		this.flash(id);
		return '';
	}

	refresh(): void {
		this.refreshing = true;
		setTimeout(() => {
			for (const a of this.accts) if (a.fetch && a.enabled) a.fetch = { minsAgo: 0, failed: false };
			this.refreshing = false;
		}, 900);
	}

	private timer: ReturnType<typeof setTimeout> | undefined;
	private flash(id: string): void {
		this.moved = id;
		clearTimeout(this.timer);
		this.timer = setTimeout(() => (this.moved = null), 900);
	}

	/** Options for the shared `reorder` action. */
	dragOpts = {
		enabled: () => this.lists[this.tab].length > 0,
		onStart: (from: number) => {
			const c = this.lists[this.tab][from];
			if (c) this.drag = { id: c.id, to: from };
		},
		onMove: (to: number) => {
			if (this.drag) this.drag.to = to;
		},
		onEnd: (from: number, to: number) => {
			this.drag = null;
			this.move(from, to);
		},
		onCancel: () => (this.drag = null)
	};
}
