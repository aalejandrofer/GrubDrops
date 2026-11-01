import { accounts, type Acct } from './_data';

type Note = { tone: 'ok' | 'wait' | 'bad'; text: string };

/** Mock page state: a fresh copy per mockup, nothing saved anywhere. */
export class PanelState {
	accts = $state<Acct[]>(structuredClone(accounts));
	/** Account whose detail view is open (A, B). */
	open = $state<string | null>(null);
	/** Rows expanded in place (C). */
	expanded = $state<string[]>([]);
	menu = $state<string | null>(null);
	confirming = $state<string | null>(null);
	renaming = $state<string | null>(null);
	renameValue = $state('');
	/** Account mid "Log in again", showing the stub flow. */
	loggingIn = $state<string | null>(null);
	/** Per account and field: saving, then saved. Key is `${id}:${field}`. */
	saved = $state<Record<string, 'wait' | 'ok'>>({});
	/** Result lines for checks, restarts and Mine now. Key is `${id}:login`, `:run` or `:mine`. */
	note = $state<Record<string, Note>>({});
	toast = $state<{ text: string; undo?: Acct; at: number; id: number } | null>(null);

	/** Latest result line for the row, whichever action made it. */
	rowNote(id: string): Note | undefined {
		return this.note[`${id}:login`] ?? this.note[`${id}:run`] ?? this.note[`${id}:mine`];
	}

	get(id: string | null): Acct | undefined {
		return id ? this.accts.find((a) => a.id === id) : undefined;
	}

	isOpen(id: string): boolean {
		return this.expanded.includes(id);
	}

	toggleRow(id: string): void {
		this.expanded = this.isOpen(id) ? this.expanded.filter((x) => x !== id) : [...this.expanded, id];
	}

	/** Steps the open account by one, for the modal arrows. */
	step(dir: -1 | 1): void {
		const i = this.accts.findIndex((a) => a.id === this.open);
		const next = this.accts[(i + dir + this.accts.length) % this.accts.length];
		if (next) this.openPanel(next.id);
	}

	openPanel(id: string): void {
		this.menu = null;
		this.confirming = null;
		this.open = id;
	}

	close(): void {
		this.open = null;
		this.confirming = null;
		this.renaming = null;
	}

	private fake(key: string): void {
		this.saved[key] = 'wait';
		setTimeout(() => {
			this.saved[key] = 'ok';
			setTimeout(() => {
				if (this.saved[key] === 'ok') delete this.saved[key];
			}, 1800);
		}, 450);
	}

	setEnabled(a: Acct, on: boolean): void {
		this.menu = null;
		a.enabled = on;
		this.fake(`${a.id}:enabled`);
	}

	setOwn(a: Acct, on: boolean): void {
		a.own_games = on;
		if (on && a.own.length === 0) a.own = ['Rust'];
		this.fake(`${a.id}:own`);
	}

	setSkip(a: Acct, on: boolean): void {
		this.menu = null;
		a.skip_sleep = on;
		this.fake(`${a.id}:skip`);
	}

	private flash(id: string, n: Note, ms = 4000): void {
		this.note[id] = n;
		if (n.tone !== 'wait')
			setTimeout(() => {
				if (this.note[id]?.text === n.text) delete this.note[id];
			}, ms);
	}

	check(a: Acct): void {
		this.menu = null;
		this.note[`${a.id}:login`] = { tone: 'wait', text: 'Checking login…' };
		setTimeout(() => {
			a.check = { ...a.check, ago: 'just now' };
			if (a.needs_login) this.flash(`${a.id}:login`, { tone: 'bad', text: `Login check failed: ${a.check.detail}` }, 8000);
			else this.flash(`${a.id}:login`, { tone: 'ok', text: 'Login is valid.' });
		}, 900);
	}

	relogin(a: Acct): void {
		this.menu = null;
		this.loggingIn = a.id;
	}

	finishLogin(a: Acct): void {
		this.loggingIn = null;
		a.needs_login = false;
		a.check = { ago: 'just now', ok: true, detail: '' };
		a.signedIn = 'Oct 31';
		this.flash(`${a.id}:login`, { tone: 'ok', text: 'Signed in. Mining resumes.' });
	}

	restart(a: Acct): void {
		this.menu = null;
		this.note[`${a.id}:run`] = { tone: 'wait', text: 'Restarting runner…' };
		setTimeout(() => this.flash(`${a.id}:run`, { tone: 'ok', text: 'Runner restarted.' }), 700);
	}

	mineNow(a: Acct): void {
		const n = a.next;
		if (!n) return;
		const d = a.doing;
		a.doing = { kind: 'mining', game: n.game, drop: n.drop, channel: a.platform === 'kick' ? 'xqc' : 'hjune', m: n.m, req: n.req };
		a.next = d.kind === 'mining' ? { game: d.game, drop: d.drop, m: d.m, req: d.req } : null;
		this.flash(`${a.id}:mine`, { tone: 'ok', text: `Now mining ${n.game} · ${n.drop}.` });
	}

	startRename(a: Acct): void {
		this.menu = null;
		this.renaming = a.id;
		this.renameValue = a.name;
	}

	commitRename(a: Acct): void {
		const v = this.renameValue.trim();
		this.renaming = null;
		const name = v || a.platform_name;
		if (name === a.name) return;
		a.name = name.slice(0, 40);
		this.fake(`${a.id}:name`);
	}

	remove(a: Acct): void {
		const i = this.accts.findIndex((x) => x.id === a.id);
		const copy = $state.snapshot(a) as Acct;
		this.accts = this.accts.filter((x) => x.id !== a.id);
		this.close();
		this.expanded = this.expanded.filter((x) => x !== a.id);
		const at = Date.now();
		this.toast = { text: `Removed ${a.name}. Its claim history stays.`, undo: copy, at: i, id: at };
		setTimeout(() => {
			if (this.toast?.id === at) this.toast = null;
		}, 7000);
	}

	undoRemove(): void {
		const t = this.toast;
		if (!t?.undo) return;
		const next = [...this.accts];
		next.splice(t.at, 0, t.undo);
		this.accts = next;
		this.toast = null;
	}
}
