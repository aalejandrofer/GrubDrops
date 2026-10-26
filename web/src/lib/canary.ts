export type RunNowState = 'running' | 'done' | 'no_result';

/** Run-now polling gives up after this long without a newer canary result. */
export const RUN_NOW_TIMEOUT_MS = 90_000;

/** Run-now progress: done once any canary was checked after the click, no_result after the timeout, else running. */
export function runNowState(clickedAt: number, canaries: { checked_at: string | null }[], now: number): RunNowState {
	if (canaries.some((c) => c.checked_at !== null && Date.parse(c.checked_at) > clickedAt)) return 'done';
	return now - clickedAt >= RUN_NOW_TIMEOUT_MS ? 'no_result' : 'running';
}
