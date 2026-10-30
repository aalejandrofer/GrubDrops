import { describe, expect, it } from 'vitest';
import type { ForceChannel } from '$lib/api/types';
import { movePlatformChannels } from './points';

const channel = (platform: ForceChannel['platform'], name: string): ForceChannel => ({
	platform,
	channel: name,
	live: null,
	accounts: [],
	watching: []
});

describe('movePlatformChannels', () => {
	it('reorders Twitch ranks without moving Kick entries or changing the global slots', () => {
		const all = [channel('twitch', 'a'), channel('kick', 'k'), channel('twitch', 'b')];
		expect(movePlatformChannels(all, 'twitch', 0, 1).map(({ platform, channel: name }) => `${platform}:${name}`)).toEqual([
			'twitch:b', 'kick:k', 'twitch:a'
		]);
	});
});
