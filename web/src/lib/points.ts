import type { ForceChannel, Platform } from '$lib/api/types';

/** Reorders one platform's rows in place among their existing global-order slots. */
export function movePlatformChannels(
	channels: readonly ForceChannel[],
	platform: Platform,
	from: number,
	to: number
): ForceChannel[] {
	const selected = channels.filter((channel) => channel.platform === platform);
	if (from < 0 || from >= selected.length || to < 0 || to >= selected.length || from === to) return [...channels];
	const [moved] = selected.splice(from, 1);
	selected.splice(to, 0, moved);
	let index = 0;
	return channels.map((channel) => channel.platform === platform ? selected[index++] : channel);
}
