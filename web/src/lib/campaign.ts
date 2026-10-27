import type { CampaignDetail, DropCell } from '$lib/api/types';

/** Watched share of a drop, 0 to 100. */
export function cellPct(cell: Pick<DropCell, 'minutes'>, required: number): number {
	if (required <= 0) return 0;
	return Math.max(0, Math.min(100, Math.round((cell.minutes / required) * 100)));
}

/** Claimed account-drop pairs across the campaign. */
export function claimedCount(c: Pick<CampaignDetail, 'drops'>): number {
	let n = 0;
	for (const d of c.drops) for (const a of d.accounts) if (a.state === 'claimed') n++;
	return n;
}

/** True when no account scans the campaign any more, or its end has passed. */
export function campaignEnded(c: Pick<CampaignDetail, 'live' | 'ends_at'>, now: number): boolean {
	if (!c.live) return true;
	return c.ends_at !== null && Date.parse(c.ends_at) <= now;
}

/** True when an account waits on an external account link. */
export function needsLink(c: Pick<CampaignDetail, 'drops'>): boolean {
	return c.drops.some((d) => d.accounts.some((a) => a.state === 'blocked' && a.reason === 'not_linked'));
}

const BLOCKED: Record<string, string> = {
	not_linked: 'campaign.blocked_link',
	precondition: 'campaign.blocked_precondition',
	skipped: 'campaign.blocked_skipped'
};

/** i18n key for a cell's status text. Accruing and claimable show minutes instead. */
export function cellKey(cell: Pick<DropCell, 'state' | 'reason'>): string {
	switch (cell.state) {
		case 'claimed':
			return 'campaign.claimed';
		case 'claimable':
			return 'campaign.claimable';
		case 'accruing':
			return 'campaign.accruing';
		case 'eligible':
			return 'campaign.not_started';
		default:
			return BLOCKED[cell.reason] ?? 'campaign.blocked';
	}
}

/** Hand actions a cell offers. Collect needs the drop in the account's runner
 * plan; uncollect needs a logged claim or a skip to remove. */
export function cellActions(c: Pick<DropCell, 'state' | 'reason' | 'queued' | 'claimed_at'>): { collect: boolean; uncollect: boolean } {
	const skipped = c.state === 'blocked' && c.reason === 'skipped';
	return {
		collect: c.state !== 'claimed' && c.queued === true,
		uncollect: (c.state === 'claimed' && c.claimed_at !== null) || skipped
	};
}

/** A hand mark the drawer shows before the board catches up with it. */
export interface PendingMark {
	account_id: string;
	drop_id: string;
	kind: 'collect' | 'uncollect';
	/** When the mark was made, ISO. */
	at: string;
	/** Epoch ms after which the fetched state wins regardless. */
	until: number;
}

/** How long a pending mark may override fetched state. */
export const PENDING_MS = 60_000;

function agrees(c: Pick<DropCell, 'state' | 'reason'>, kind: PendingMark['kind']): boolean {
	if (kind === 'collect') return c.state === 'claimed';
	return c.state !== 'claimed' && !(c.state === 'blocked' && c.reason === 'skipped');
}

function findCell(d: Pick<CampaignDetail, 'drops'>, m: PendingMark): DropCell | undefined {
	return d.drops.find((x) => x.id === m.drop_id)?.accounts.find((c) => c.account_id === m.account_id);
}

/** Pending marks still worth showing: not expired, cell present and the
 * fetched state does not show the mark yet. */
export function settlePending(d: Pick<CampaignDetail, 'drops'>, pending: PendingMark[], now: number): PendingMark[] {
	return pending.filter((m) => {
		if (now >= m.until) return false;
		const c = findCell(d, m);
		return !!c && !agrees(c, m.kind);
	});
}

/** The detail with pending marks applied over the fetched cells. */
export function withPending(d: CampaignDetail, pending: PendingMark[]): CampaignDetail {
	if (pending.length === 0) return d;
	return {
		...d,
		drops: d.drops.map((drop) => ({
			...drop,
			accounts: drop.accounts.map((c) => {
				const m = pending.find((x) => x.drop_id === drop.id && x.account_id === c.account_id);
				if (!m) return c;
				if (m.kind === 'collect')
					return {
						...c,
						state: 'claimed',
						reason: '',
						queued: false,
						claimed_at: c.claimed_at ?? m.at,
						minutes: Math.max(c.minutes, drop.required_minutes)
					};
				return {
					...c,
					state: 'eligible',
					reason: '',
					queued: false,
					claimed_at: null
				};
			})
		}))
	};
}
