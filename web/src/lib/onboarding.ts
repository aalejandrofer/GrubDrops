import type { Account } from '$lib/api/types';
import type { HomeView } from '$lib/home';

/** First-run wizard: 1 add accounts, 2 pick games. Start mining then goes Home. */
export type Step = 1 | 2;

export const STEPS: Step[] = [1, 2];

/** Home sends a first run, no accounts yet, to the wizard. */
export function wantsWizard(view: HomeView): boolean {
	return view === 'first_run';
}

/**
 * The step the wizard opens on, from signed-in accounts and chosen games.
 * A half-made account left by a reload does not count. null means both
 * exist, so the wizard has nothing left and Home applies.
 */
export function startStep(signedIn: number, games: number): Step | null {
	if (signedIn === 0) return 1;
	if (games === 0) return 2;
	return null;
}

/** Accounts with a session. A half-made account from an open flow needs a login, so it does not count. */
export function signedIn(accounts: Account[]): Account[] {
	return accounts.filter((a) => !a.needs_login);
}

/** Continue is open: step 1 needs a signed-in account, step 2 a game. */
export function canContinue(step: Step, c: { signedIn: number; games: number }): boolean {
	if (step === 1) return c.signedIn > 0;
	return c.games > 0;
}

/** Segment states for the progress bar. */
export function segments(step: Step): ('done' | 'cur' | 'todo')[] {
	return STEPS.map((s) => (s < step ? 'done' : s === step ? 'cur' : 'todo'));
}
