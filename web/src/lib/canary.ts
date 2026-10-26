export type RunNowState = 'running' | 'done' | 'no_result';

/** Run-now polling gives up after this long without a changed canary result. */
export const RUN_NOW_TIMEOUT_MS = 90_000;

/** Each platform's `checked_at` as last seen, keyed by platform. */
export type CanaryStamps = Record<string, string | null>;

/**
 * Run-now progress. Done once any platform's `checked_at` differs from the snapshot taken before the click
 * (server clock only, so browser clock skew cannot matter); no_result once `elapsedMs` (browser clock) hits the timeout.
 */
export function runNowState(
	before: CanaryStamps,
	canaries: { platform: string; checked_at: string | null }[],
	elapsedMs: number
): RunNowState {
	if (canaries.some((c) => c.checked_at !== null && c.checked_at !== (before[c.platform] ?? null))) return 'done';
	return elapsedMs >= RUN_NOW_TIMEOUT_MS ? 'no_result' : 'running';
}
