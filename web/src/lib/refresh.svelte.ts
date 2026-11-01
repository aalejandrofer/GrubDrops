import { untrack } from 'svelte';
import { ApiError } from '$lib/api/client';

/** How long a refresh stays busy when no fresh data lands. */
export const REFRESH_WAIT_MS = 30_000;
/** How long the button shows green once fresh data landed. */
export const REFRESH_DONE_MS = 5_000;

export interface RefreshOpts {
	/** Starts the refresh on the server. */
	post: () => Promise<void>;
	/** A value that changes when fresh data lands, such as a store's last response. */
	signal: () => unknown;
	/** Runs once when fresh data landed after a refresh started. */
	onland?: () => void;
	waitMs?: number;
}

/**
 * A refresh button's state machine. Busy from run() until the signal
 * changes (then done for REFRESH_DONE_MS) or waitMs passes (then idle).
 * The server takes one refresh a minute, so a 429 means the data is under a
 * minute old: it counts as landed. Any other error sets failed.
 *
 * check() compares the signal with its value at start; createRefresh calls
 * it from an effect so it runs whenever the signal changes.
 */
export class RefreshState {
	/** The signal at start, boxed so an undefined signal still counts as busy. */
	#from = $state.raw<{ v: unknown } | null>(null);
	#done = $state(false);
	#failed = $state(false);
	#waitTimer: ReturnType<typeof setTimeout> | undefined;
	#doneTimer: ReturnType<typeof setTimeout> | undefined;
	readonly #opts: RefreshOpts;

	constructor(opts: RefreshOpts) {
		this.#opts = opts;
	}

	/** Started and waiting for fresh data. */
	get busy(): boolean {
		return this.#from !== null;
	}
	/** Fresh data landed in the last REFRESH_DONE_MS. */
	get done(): boolean {
		return this.#done;
	}
	/** The last start failed. */
	get failed(): boolean {
		return this.#failed;
	}

	/** Lands the refresh once the signal moved off its value at start. */
	check(): void {
		const f = this.#from;
		if (f === null) return;
		if (this.#opts.signal() === f.v) return;
		untrack(() => this.#land());
	}

	async run(): Promise<void> {
		if (this.#from !== null) return;
		this.#failed = false;
		this.#done = false;
		clearTimeout(this.#doneTimer);
		const mine = { v: untrack(this.#opts.signal) };
		this.#from = mine;
		try {
			await this.#opts.post();
		} catch (e) {
			if (this.#from !== mine) return;
			if (e instanceof ApiError && e.status === 429) this.#land();
			else {
				this.#from = null;
				this.#failed = true;
			}
			return;
		}
		if (this.#from !== mine) return;
		clearTimeout(this.#waitTimer);
		this.#waitTimer = setTimeout(() => {
			if (this.#from === mine) this.#from = null;
		}, this.#opts.waitMs ?? REFRESH_WAIT_MS);
	}

	/** Stops every timer. */
	dispose(): void {
		clearTimeout(this.#waitTimer);
		clearTimeout(this.#doneTimer);
	}

	#land(): void {
		this.#from = null;
		clearTimeout(this.#waitTimer);
		this.#done = true;
		clearTimeout(this.#doneTimer);
		this.#doneTimer = setTimeout(() => (this.#done = false), REFRESH_DONE_MS);
		this.#opts.onland?.();
	}
}

/**
 * A refresh button's state, wired to the signal. Call it while a component
 * initialises: it owns effects.
 */
export function createRefresh(opts: RefreshOpts): RefreshState {
	const r = new RefreshState(opts);
	$effect(() => r.check());
	$effect(() => () => r.dispose());
	return r;
}
