/** Typed Settings fields save on their own this long after the last edit. */
export const AUTOSAVE_MS = 10_000;

/** How long "Saved" stays up after an auto-save. */
export const SAVED_HOLD_MS = 2000;

export type Phase = 'idle' | 'wait' | 'saving' | 'saved';

/** Saves one field the way its Save button does; false when it was invalid or the save failed. */
type Run = () => Promise<boolean> | boolean;

interface Entry {
	run: Run;
	valid: () => boolean;
}

/**
 * One countdown for every pending field on a page. Each edit restarts it; at 0
 * every pending field saves. A field that is invalid or fails to save stays
 * pending, without a countdown, until it is edited or saved again.
 */
export class Autosave {
	#entries = new Map<string, Entry>();
	#ver = new Map<string, number>();
	#timer: ReturnType<typeof setTimeout> | undefined;
	#hold: ReturnType<typeof setTimeout> | undefined;
	#onChange: () => void;
	#ms: number;
	phase: Phase = 'idle';
	/** When the countdown ends, 0 when none runs. */
	dueAt = 0;

	constructor(onChange: () => void = () => {}, ms = AUTOSAVE_MS) {
		this.#onChange = onChange;
		this.#ms = ms;
	}

	/** A field changed: keep its save and restart the countdown. */
	edit(key: string, run: Run, valid: () => boolean = () => true): void {
		this.#entries.set(key, { run, valid });
		this.#bump(key);
		clearTimeout(this.#timer);
		clearTimeout(this.#hold);
		this.dueAt = Date.now() + this.#ms;
		this.phase = 'wait';
		this.#timer = setTimeout(() => void this.flush(), this.#ms);
		this.#onChange();
	}

	/** Forget a field's pending save, e.g. on Cancel or once it is cleared. */
	drop(key: string): void {
		if (!this.#entries.delete(key)) return;
		this.#bump(key);
		if (this.#entries.size === 0) this.#stop();
		this.#onChange();
	}

	/** Whether a field, or any field, has a pending save. */
	has(key?: string): boolean {
		return key === undefined ? this.#entries.size > 0 : this.#entries.has(key);
	}

	/** Pending fields that would not save as they stand. */
	invalid(): string[] {
		return [...this.#entries].filter(([, e]) => !e.valid()).map(([k]) => k);
	}

	/** Save one field now (its Save button or Enter); the others keep counting down. */
	async now(key: string, run: Run, valid: () => boolean = () => true): Promise<boolean> {
		const e = this.#entries.get(key) ?? { run, valid };
		this.#entries.delete(key);
		if (this.#entries.size === 0) this.#stop();
		this.#onChange();
		return this.#run(key, { run, valid: e.valid });
	}

	/**
	 * Save every pending field now. Runs start before this returns its promise,
	 * so a page that is unloading can call it without waiting. One after the
	 * other by default; all at once with parallel.
	 */
	async flush(parallel = false): Promise<boolean> {
		clearTimeout(this.#timer);
		this.dueAt = 0;
		const all = [...this.#entries];
		if (all.length === 0) {
			this.#stop();
			return true;
		}
		this.#entries.clear();
		clearTimeout(this.#hold);
		this.phase = 'saving';
		this.#onChange();
		let ok = true;
		if (parallel) {
			ok = (await Promise.all(all.map(([k, e]) => this.#run(k, e)))).every(Boolean);
		} else {
			for (const [k, e] of all) ok = (await this.#run(k, e)) && ok;
		}
		if (this.phase !== 'saving') return ok;
		if (ok && this.#entries.size === 0) {
			this.phase = 'saved';
			this.#hold = setTimeout(() => {
				this.phase = 'idle';
				this.#onChange();
			}, SAVED_HOLD_MS);
		} else {
			this.phase = 'idle';
		}
		this.#onChange();
		return ok;
	}

	/** Stop the timers, e.g. when the page goes away. */
	dispose(): void {
		clearTimeout(this.#timer);
		clearTimeout(this.#hold);
	}

	/** Whole seconds left on the countdown at now. */
	secondsLeft(now: number): number {
		return this.dueAt > 0 ? Math.max(0, Math.ceil((this.dueAt - now) / 1000)) : 0;
	}

	async #run(key: string, e: Entry): Promise<boolean> {
		const ver = this.#ver.get(key) ?? 0;
		let ok = false;
		try {
			ok = await e.run();
		} catch {
			ok = false;
		}
		// Kept unless the field was edited, saved or dropped since.
		if (!ok && this.#ver.get(key) === ver && !this.#entries.has(key)) {
			this.#entries.set(key, e);
			this.#onChange();
		}
		return ok;
	}

	#bump(key: string): void {
		this.#ver.set(key, (this.#ver.get(key) ?? 0) + 1);
	}

	#stop(): void {
		clearTimeout(this.#timer);
		this.dueAt = 0;
		if (this.phase === 'wait') this.phase = 'idle';
	}
}

/**
 * Whether an input event came from the user typing, pasting or deleting.
 * Browser autofill sends no inputType, or insertReplacementText, so a
 * password manager filling a secret field never starts a save.
 */
export function typed(e: { isTrusted: boolean; inputType?: string }): boolean {
	return e.isTrusted && !!e.inputType && e.inputType !== 'insertReplacementText';
}
