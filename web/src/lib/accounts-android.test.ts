import { describe, expect, it } from 'vitest';
import { androidErrorKey, hasAndroidToken, toBase64 } from './accounts';

const TOK = 'abcdefghijklmnopqrstuvwxyz0123';

describe('hasAndroidToken', () => {
	it('accepts the redirect URL with extra params', () => {
		expect(hasAndroidToken(`https://www.twitch.tv/#access_token=${TOK}&scope=user_read&token_type=bearer`)).toBe(true);
		expect(hasAndroidToken(`https://www.twitch.tv/#scope=&access_token=${TOK}`)).toBe(true);
		expect(hasAndroidToken(`  https://www.twitch.tv/#access_token=${TOK}\n`)).toBe(true);
	});
	it('accepts a bare token', () => {
		expect(hasAndroidToken(TOK)).toBe(true);
		expect(hasAndroidToken(` ${TOK} `)).toBe(true);
	});
	it('rejects garbage', () => {
		expect(hasAndroidToken('')).toBe(false);
		expect(hasAndroidToken('https://www.twitch.tv/')).toBe(false);
		expect(hasAndroidToken('https://www.twitch.tv/?error=access_denied')).toBe(false);
		expect(hasAndroidToken('hello world')).toBe(false);
		expect(hasAndroidToken('abc')).toBe(false);
		expect(hasAndroidToken(`https://x.test/#my_access_token=${TOK}`)).toBe(false);
	});
});

describe('androidErrorKey', () => {
	it('maps API codes', () => {
		expect(androidErrorKey('android_no_token')).toBe('accounts.android_no_token');
		expect(androidErrorKey('android_wrong_client')).toBe('accounts.android_wrong_client');
		expect(androidErrorKey('android_invalid_token')).toBe('accounts.android_invalid_token');
		expect(androidErrorKey('tdm_no_token')).toBe('accounts.tdm_no_token');
	});
	it('falls back for anything else', () => {
		expect(androidErrorKey('upstream')).toBe('accounts.android_failed');
		expect(androidErrorKey(undefined)).toBe('accounts.android_failed');
		expect(androidErrorKey('toString')).toBe('accounts.android_failed');
	});
});

describe('toBase64', () => {
	it('encodes binary bytes', () => {
		expect(toBase64(new Uint8Array([0x80, 0x05, 0x95, 0x00, 0xff]))).toBe('gAWVAP8=');
		expect(toBase64(new Uint8Array())).toBe('');
	});
	it('encodes past one chunk', () => {
		const big = new Uint8Array(0x8000 * 2 + 3).fill(0x41);
		expect(atob(toBase64(big))).toBe('A'.repeat(big.length));
	});
});
