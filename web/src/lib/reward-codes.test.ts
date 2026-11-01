import { describe, expect, it } from 'vitest';
import type { RewardCode } from '$lib/api/types';
import { expiresIn, liveCodes, redeemHref } from './reward-codes';

const now = Date.parse('2026-11-01T12:00:00Z');

function code(id: number, expires_at: string | null): RewardCode {
	return {
		id,
		account_id: 'a1',
		account_name: 'Alice',
		name: 'Cape',
		game: 'Minecraft',
		code: 'AB12-CD34-EF56',
		redeem_url: 'https://www.minecraft.net/redeem',
		expires_at,
		received_at: null,
		first_seen: null
	};
}

describe('expiresIn', () => {
	it('counts whole days from one day up', () => {
		expect(expiresIn('2026-11-04T18:00:00Z', now)).toEqual({ key: 'rewards.expires_days', n: 3 });
		expect(expiresIn('2026-11-02T12:00:00Z', now)).toEqual({ key: 'rewards.expires_days', n: 1 });
	});
	it('counts hours under a day, at least one', () => {
		expect(expiresIn('2026-11-02T05:30:00Z', now)).toEqual({ key: 'rewards.expires_hours', n: 17 });
		expect(expiresIn('2026-11-01T12:10:00Z', now)).toEqual({ key: 'rewards.expires_hours', n: 1 });
	});
	it('is null without a future expiry', () => {
		expect(expiresIn(null, now)).toBeNull();
		expect(expiresIn('2026-11-01T11:00:00Z', now)).toBeNull();
		expect(expiresIn('soon', now)).toBeNull();
	});
});

describe('liveCodes', () => {
	it('drops expired codes and keeps ones without expiry', () => {
		const list = [code(1, null), code(2, '2026-11-01T11:59:00Z'), code(3, '2026-11-05T00:00:00Z')];
		expect(liveCodes(list, now).map((c) => c.id)).toEqual([1, 3]);
	});
});

describe('redeemHref', () => {
	it('accepts only http(s) links', () => {
		expect(redeemHref('https://www.twitch.tv/drops/inventory')).toBe('https://www.twitch.tv/drops/inventory');
		expect(redeemHref('javascript:alert(1)')).toBeNull();
		expect(redeemHref('')).toBeNull();
		expect(redeemHref('https://x y')).toBeNull();
	});
});
