import type { Overview } from '$lib/api/types';
import { t, tn } from '$lib/i18n/index.svelte';

export type Tone = 'green' | 'amber' | 'red' | 'grey';

export function headerTone(o: Overview | null, unreachable: boolean): Tone {
	if (unreachable) return 'red';
	if (o?.status.state === 'mining') return o.problems.length > 0 ? 'amber' : 'green';
	return o && o.problems.length > 0 ? 'amber' : 'grey';
}

export function statusText(o: Overview | null, unreachable: boolean): string {
	if (unreachable) return t('status.offline');
	if (!o) return '';
	switch (o.status.state) {
		case 'no_accounts':
		case 'no_games':
			return t('status.not_set_up');
		case 'mining':
			return tn('status.mining', o.status.accounts_total, { m: o.status.accounts_mining });
		case 'nothing_live':
			return t('status.nothing_live');
		default:
			return t('status.idle');
	}
}

/** skip is an account mid-login: its needs-login problem is expected, not news. */
export function problemChip(o: Overview | null, skip: string | null = null): { text: string; href: string } | null {
	const n = o?.problems.filter((p) => p.kind === 'needs_login' && p.account_id !== skip).length ?? 0;
	if (n === 0) return null;
	return { text: tn('status.needs_login', n), href: '/settings#accounts' };
}
