import { describe, expect, it } from 'vitest';
import type { CampaignDetail, CampaignDrop, DropCell } from '$lib/api/types';
import {
	campaignEnded,
	cellActions,
	cellKey,
	cellPct,
	claimedCount,
	needsLink,
	settlePending,
	withPending,
	type PendingMark
} from './campaign';

const cell = (state: DropCell['state'], reason = '', minutes = 0): DropCell => ({
	account_id: 'a',
	account_name: 'a',
	platform: 'twitch',
	state,
	reason,
	minutes,
	claimed_at: null,
	queued: false
});
const drop = (...accounts: DropCell[]): CampaignDrop => ({
	id: 'd',
	name: 'd',
	image_url: '',
	required_minutes: 60,
	accounts
});

describe('campaign helpers', () => {
	it('cellPct clamps and handles zero required', () => {
		expect(cellPct({ minutes: 30 }, 60)).toBe(50);
		expect(cellPct({ minutes: 90 }, 60)).toBe(100);
		expect(cellPct({ minutes: 5 }, 0)).toBe(0);
	});

	it('claimedCount counts claimed cells over all drops', () => {
		expect(
			claimedCount({
				drops: [drop(cell('claimed'), cell('accruing')), drop(cell('claimed'))]
			})
		).toBe(2);
		expect(claimedCount({ drops: [] })).toBe(0);
	});

	it('campaignEnded when not live or past its end', () => {
		const now = Date.parse('2026-10-27T12:00:00Z');
		expect(campaignEnded({ live: false, ends_at: null }, now)).toBe(true);
		expect(campaignEnded({ live: true, ends_at: '2026-10-27T11:00:00Z' }, now)).toBe(true);
		expect(campaignEnded({ live: true, ends_at: '2026-10-28T00:00:00Z' }, now)).toBe(false);
		expect(campaignEnded({ live: true, ends_at: null }, now)).toBe(false);
	});

	it('needsLink only for blocked not_linked cells', () => {
		expect(needsLink({ drops: [drop(cell('blocked', 'not_linked'))] })).toBe(true);
		expect(
			needsLink({
				drops: [drop(cell('blocked', 'precondition'), cell('eligible'))]
			})
		).toBe(false);
	});

	it('cellKey maps states and blocked reasons', () => {
		expect(cellKey(cell('claimed'))).toBe('campaign.claimed');
		expect(cellKey(cell('eligible'))).toBe('campaign.not_started');
		expect(cellKey(cell('blocked', 'not_linked'))).toBe('campaign.blocked_link');
		expect(cellKey(cell('blocked', 'precondition'))).toBe('campaign.blocked_precondition');
		expect(cellKey(cell('blocked', 'weird'))).toBe('campaign.blocked');
	});

	it('cellActions: collect needs the plan, uncollect a logged claim or a skip', () => {
		const q = (c: DropCell): DropCell => ({ ...c, queued: true });
		expect(cellActions(q(cell('eligible')))).toEqual({
			collect: true,
			uncollect: false
		});
		expect(cellActions(q(cell('accruing')))).toEqual({
			collect: true,
			uncollect: false
		});
		expect(cellActions(q(cell('blocked', 'not_linked')))).toEqual({
			collect: true,
			uncollect: false
		});
		expect(cellActions(cell('accruing'))).toEqual({
			collect: false,
			uncollect: false
		});
		expect(cellActions(q(cell('blocked', 'skipped')))).toEqual({
			collect: true,
			uncollect: true
		});
		expect(cellActions(cell('blocked', 'skipped'))).toEqual({
			collect: false,
			uncollect: true
		});
		expect(cellActions({ ...cell('claimed'), claimed_at: '2026-10-27T00:00:00Z' })).toEqual({ collect: false, uncollect: true });
		// Claimed on the platform only: nothing logged to remove.
		expect(cellActions(q(cell('claimed')))).toEqual({
			collect: false,
			uncollect: false
		});
	});

	it('pending marks override cells until the fetch agrees or they expire', () => {
		const detail = (...accounts: DropCell[]): CampaignDetail => ({
			id: 'c',
			platform: 'twitch',
			game: 'g',
			name: 'c',
			box_art_url: '',
			starts_at: null,
			ends_at: null,
			live: true,
			link_url: '',
			drops: [drop(...accounts)]
		});
		const mk = (kind: PendingMark['kind'], until = 1000): PendingMark => ({
			account_id: 'a',
			drop_id: 'd',
			kind,
			at: '2026-10-27T00:00:00Z',
			until
		});

		const skipped = detail({ ...cell('blocked', 'skipped'), queued: true });
		const un = withPending(skipped, [mk('uncollect')]).drops[0].accounts[0];
		expect(un).toMatchObject({
			state: 'eligible',
			reason: '',
			queued: false,
			claimed_at: null
		});
		expect(settlePending(skipped, [mk('uncollect')], 0)).toHaveLength(1);
		expect(settlePending(detail(cell('accruing', '', 5)), [mk('uncollect')], 0)).toHaveLength(0);
		expect(settlePending(skipped, [mk('uncollect')], 1000), 'expired').toHaveLength(0);

		const col = withPending(detail({ ...cell('accruing', '', 5), queued: true }), [mk('collect')]).drops[0].accounts[0];
		expect(col).toMatchObject({
			state: 'claimed',
			minutes: 60,
			queued: false,
			claimed_at: '2026-10-27T00:00:00Z'
		});
		expect(settlePending(detail({ ...cell('claimed'), claimed_at: 'x' }), [mk('collect')], 0)).toHaveLength(0);
		expect(settlePending(detail(), [mk('collect')], 0), 'cell gone').toHaveLength(0);
		expect(withPending(skipped, [])).toBe(skipped);
	});
});
