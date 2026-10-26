import { Unreachable } from '$lib/api/client';

export interface RetryOpts {
	ms?: number;
	/** Run fn once after ms; returns a cancel function. */
	schedule?: (fn: () => void, ms: number) => () => void;
	/** Run fn every ms; returns a cancel function. Drives the countdown. */
	tick?: (fn: () => void, ms: number) => () => void;
	/** Called with true and the seconds until the next try when fn is Unreachable, false once it succeeds. */
	onState?: (down: boolean, retryIn: number) => void;
}

const defaultSchedule = (fn: () => void, ms: number) => {
	const id = setTimeout(fn, ms);
	return () => clearTimeout(id);
};
const defaultTick = (fn: () => void, ms: number) => {
	const id = setInterval(fn, ms);
	return () => clearInterval(id);
};

/**
 * Run fn, and while it throws Unreachable run it again every `ms`.
 * `done` resolves true on success, false after stop(); other errors reject.
 */
export function retryUnreachable(
	fn: () => Promise<void>,
	opts: RetryOpts = {}
): { done: Promise<boolean>; stop: () => void } {
	const ms = opts.ms ?? 5000;
	const schedule = opts.schedule ?? defaultSchedule;
	const tick = opts.tick ?? defaultTick;
	let stopped = false;
	let cancelTimer: (() => void) | null = null;
	let cancelTick: (() => void) | null = null;
	let resolve!: (ok: boolean) => void;
	let reject!: (e: unknown) => void;
	const done = new Promise<boolean>((res, rej) => {
		resolve = res;
		reject = rej;
	});

	function clear(): void {
		cancelTimer?.();
		cancelTimer = null;
		cancelTick?.();
		cancelTick = null;
	}

	async function attempt(): Promise<void> {
		if (stopped) return;
		clear();
		try {
			await fn();
		} catch (e) {
			if (stopped) return;
			if (!(e instanceof Unreachable)) return reject(e);
			let left = Math.round(ms / 1000);
			opts.onState?.(true, left);
			cancelTick = tick(() => {
				if (left > 0) opts.onState?.(true, --left);
			}, 1000);
			cancelTimer = schedule(() => void attempt(), ms);
			return;
		}
		if (stopped) return;
		opts.onState?.(false, 0);
		resolve(true);
	}

	void attempt();
	return {
		done,
		stop() {
			stopped = true;
			clear();
			resolve(false);
		}
	};
}
