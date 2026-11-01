import { describe, expect, it } from 'vitest';
import { slugRange, withScheme } from './hint';

describe('slugRange', () => {
	it('selects the game slug of a Kick category link', () => {
		const url = 'https://kick.com/category/rust';
		const [a, b] = slugRange(url);
		expect(url.slice(a, b)).toBe('rust');
		expect(b).toBe(url.length);
	});

	it('selects the game slug of a Twitch category link', () => {
		const url = 'https://www.twitch.tv/directory/category/rust';
		const [a, b] = slugRange(url);
		expect(url.slice(a, b)).toBe('rust');
	});

	it('selects the streamer of a channel link', () => {
		for (const url of ['https://www.twitch.tv/streamer', 'https://kick.com/streamer']) {
			const [a, b] = slugRange(url);
			expect(url.slice(a, b)).toBe('streamer');
		}
	});

	it('ignores a trailing slash', () => {
		const url = 'https://kick.com/category/rust/';
		const [a, b] = slugRange(url);
		expect(url.slice(a, b)).toBe('rust');
	});

	it('selects the whole text when there is no slash', () => {
		expect(slugRange('rust')).toEqual([0, 4]);
	});
});

describe('withScheme', () => {
	it('fills an empty field with the scheme', () => {
		expect(withScheme('', 'socks5://')).toBe('socks5://');
	});

	it('swaps the scheme and keeps the rest', () => {
		expect(withScheme('http://user:pass@127.0.0.1:1080', 'socks5://')).toBe('socks5://user:pass@127.0.0.1:1080');
	});

	it('adds the scheme to a bare host', () => {
		expect(withScheme(' 127.0.0.1:8080 ', 'https://')).toBe('https://127.0.0.1:8080');
	});
});
