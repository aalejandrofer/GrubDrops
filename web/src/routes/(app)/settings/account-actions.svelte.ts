import { api } from '$lib/app';
import { ApiError } from '$lib/api/client';
import type { Account, PointsMode } from '$lib/api/types';
import { fieldKey, ROW_NOTES, type Field, type NoteKind } from '$lib/account-panel';
import { renameRequest } from '$lib/accounts';
import { t } from '$lib/i18n/index.svelte';
import type { LiveStore } from '$lib/stores/live.svelte';
import { commitMineNow, mineErrorKey } from '$lib/upnext';

export type Note = { tone: 'ok' | 'wait' | 'bad'; text: string };
export type Mark = 'wait' | 'ok' | 'bad';

/** A saved mark lingers this long; a success note this long. Failures stay until the next try. */
const SAVED_MS = 1800;
const NOTE_MS = 6000;

function errMsg(e: unknown, fallback: string): string {
	return e instanceof ApiError && e.body.message ? e.body.message : fallback;
}

/**
 * Settings > Accounts state shared by the rows, their ⋯ menus and the detail
 * drawer. Every action calls the API; the server decides whether the runner
 * restarts, and the live store's events carry the result back.
 */
export class AccountActions {
	/** Account whose drawer is open. */
	open = $state<string | null>(null);
	/** Last account the drawer showed, for focus on close. */
	last = $state<string | null>(null);
	menu = $state<string | null>(null);
	/** Account showing its sign-in flow in the drawer. */
	relogin = $state<string | null>(null);
	confirming = $state<string | null>(null);
	renaming = $state<string | null>(null);
	renameValue = $state('');
	/** Saving, saved or failed per field, keyed by fieldKey. */
	marks = $state<Record<string, Mark>>({});
	/** Result lines, keyed `${id}:${kind}` with a NoteKind or a Field. */
	notes = $state<Record<string, Note>>({});

	private live: LiveStore;

	constructor(live: LiveStore) {
		this.live = live;
	}

	note(id: string, kind: NoteKind | Field): Note | undefined {
		return this.notes[`${id}:${kind}`];
	}

	/** The line a row shows while its drawer is closed. */
	rowNote(id: string): Note | undefined {
		for (const k of ROW_NOTES) {
			const n = this.notes[`${id}:${k}`];
			if (n) return n;
		}
		return undefined;
	}

	openPanel(id: string): void {
		this.menu = null;
		if (this.open !== id) {
			this.confirming = null;
			this.renaming = null;
			if (this.relogin !== id) this.relogin = null;
		}
		this.open = id;
		this.last = id;
	}

	close(): void {
		this.open = null;
		this.confirming = null;
		this.renaming = null;
		this.relogin = null;
	}

	/** Opens the drawer on the account's sign-in flow. */
	startLogin(a: Account): void {
		this.openPanel(a.id);
		this.relogin = a.id;
	}

	private flash(key: string, n: Note): void {
		this.notes[key] = n;
		if (n.tone === 'ok')
			setTimeout(() => {
				if (this.notes[key]?.text === n.text) delete this.notes[key];
			}, NOTE_MS);
	}

	/** Runs one save with the saving, saved or failed mark; undo puts the old value back on failure. */
	private async save(a: Account, f: Field, run: () => Promise<void>, fail: string, undo?: () => void): Promise<boolean> {
		const k = fieldKey(a.id, f);
		this.menu = null;
		this.marks[k] = 'wait';
		delete this.notes[k];
		try {
			await run();
		} catch (e) {
			undo?.();
			this.marks[k] = 'bad';
			this.notes[k] = { tone: 'bad', text: errMsg(e, fail) };
			return false;
		}
		this.marks[k] = 'ok';
		setTimeout(() => {
			if (this.marks[k] === 'ok') delete this.marks[k];
		}, SAVED_MS);
		return true;
	}

	private refetch(): Promise<void> {
		return this.live.refetchAll().catch(() => {});
	}

	setEnabled(a: Account, on: boolean): Promise<boolean> {
		const prev = a.enabled;
		a.enabled = on;
		return this.save(
			a,
			'enabled',
			async () => {
				await api.setAccountEnabled(a.id, on);
				await this.refetch();
			},
			t('accounts.pause_failed', { name: a.name }),
			() => (a.enabled = prev)
		);
	}

	setSkipSleep(a: Account, on: boolean): Promise<boolean> {
		const prev = a.skip_sleep;
		a.skip_sleep = on;
		return this.save(
			a,
			'skip',
			async () => {
				await api.setSkipSleep(a.id, on);
				await this.refetch();
			},
			t('accounts.pause_failed', { name: a.name }),
			() => (a.skip_sleep = prev)
		);
	}

	setPointsMode(a: Account, mode: PointsMode): Promise<boolean> {
		const prev = a.points_mode;
		a.points_mode = mode;
		return this.save(
			a,
			'points',
			() => api.setPointsMode(a.id, mode),
			t('accounts.points_failed', { name: a.name }),
			() => (a.points_mode = prev)
		);
	}

	/** Back to the shared list; the stored own list stays for next time. */
	ownOff(a: Account): Promise<boolean> {
		return this.save(
			a,
			'own',
			async () => {
				await api.setAccountGames(a.id, false, null);
				await this.refreshAccounts();
			},
			t('accounts.games_failed')
		);
	}

	/** The own list editor saved: show the mark and take the new count. */
	async ownSaved(a: Account): Promise<void> {
		await this.save(a, 'own', () => this.refreshAccounts(), t('accounts.games_failed'));
	}

	private async refreshAccounts(): Promise<void> {
		try {
			this.live.accounts = await api.accounts();
		} catch {
			/* the accounts event refetches */
		}
	}

	startRename(a: Account): void {
		this.renaming = a.id;
		this.renameValue = a.name;
		delete this.notes[fieldKey(a.id, 'name')];
	}

	cancelRename(): void {
		this.renaming = null;
	}

	async commitRename(a: Account): Promise<void> {
		const req = renameRequest(a, this.renameValue);
		const k = fieldKey(a.id, 'name');
		if (req.kind === 'too_long') {
			this.notes[k] = { tone: 'bad', text: t('accounts.rename_too_long') };
			return;
		}
		this.renaming = null;
		if (req.kind === 'skip') return;
		await this.save(a, 'name', () => api.renameAccount(a.id, req.name), t('accounts.rename_failed'));
	}

	async check(a: Account): Promise<void> {
		const k = `${a.id}:login`;
		this.menu = null;
		this.notes[k] = { tone: 'wait', text: t('accounts.checking') };
		try {
			const res = await api.checkLogin(a.id);
			if (res.ok) this.flash(k, { tone: 'ok', text: t('accounts.check_ok') });
			else this.notes[k] = { tone: 'bad', text: t('accounts.check_bad', { detail: res.detail }) };
		} catch (e) {
			this.notes[k] = { tone: 'bad', text: errMsg(e, t('accounts.check_failed')) };
		}
	}

	async restart(a: Account): Promise<void> {
		const k = `${a.id}:run`;
		this.menu = null;
		this.notes[k] = { tone: 'wait', text: t('accounts.restarting') };
		try {
			await api.restartAccount(a.id);
			this.flash(k, { tone: 'ok', text: t('accounts.restarted') });
		} catch (e) {
			this.notes[k] = { tone: 'bad', text: errMsg(e, t('accounts.restart_failed')) };
		}
	}

	/** Same request and optimistic switch as Mine next on Home. */
	async mineNow(a: Account): Promise<void> {
		const k = `${a.id}:mine`;
		const next = this.live.runners.find((r) => r.account_id === a.id)?.up_next[0];
		if (!next || this.notes[k]?.tone === 'wait') return;
		this.notes[k] = { tone: 'wait', text: t('accounts.mine_starting') };
		let why = 'upnext.mine_next_failed';
		const ok = await commitMineNow(
			{
				get: () => this.live.runners,
				set: (r) => (this.live.runners = r),
				mineNow: (id) =>
					api.mineNow(id).catch((e: unknown) => {
						why = mineErrorKey(e);
						throw e;
					}),
				fetchRunners: () => api.runners()
			},
			a.id
		);
		if (ok) this.flash(k, { tone: 'ok', text: t('accounts.mine_now_ok', { game: next.game, drop: next.benefit_name }) });
		else this.notes[k] = { tone: 'bad', text: t(why) };
	}

	async remove(a: Account): Promise<void> {
		const k = `${a.id}:remove`;
		delete this.notes[k];
		try {
			await api.removeAccount(a.id);
		} catch (e) {
			this.notes[k] = { tone: 'bad', text: errMsg(e, t('accounts.remove_failed')) };
			return;
		}
		if (this.open === a.id) this.close();
		this.confirming = null;
		await this.refetch();
	}

	/** The sign-in flow finished: close it and show the result under Login. */
	async loggedIn(a: Account): Promise<void> {
		this.relogin = null;
		await this.refetch();
		this.flash(`${a.id}:login`, { tone: 'ok', text: t('accounts.device_done') });
	}
}
