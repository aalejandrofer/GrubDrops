<script lang="ts">
	import type { Platform } from '$lib/api/types';

	let { platform, size = 'md' }: { platform: Platform; size?: 'md' | 'sm' } = $props();
	const PATHS: Record<Platform, string> = {
		twitch: 'M4 2 2 6v14h5v3h3l3-3h4l5-5V2H4zm16 12-3 3h-5l-3 3v-3H5V4h15v10zM15 7h2v5h-2zm-5 0h2v5h-2z',
		kick: 'M3 2h6v6h2V5h3V2h7v7h-3v3h3v3h3v7h-7v-3h-3v-3H9v6H3z'
	};
</script>

<span class="pm {platform}" class:sm={size === 'sm'} role="img" aria-label={platform}>
	{#if size === 'md'}<svg viewBox="0 0 24 24" aria-hidden="true"><path d={PATHS[platform]} fill="currentColor" /></svg>{/if}
</span>

<style>
	.pm {
		width: 14px;
		height: 14px;
		border-radius: 3px;
		display: inline-grid;
		place-items: center;
		flex: none;
	}
	.pm svg {
		width: 10px;
		height: 10px;
	}
	.twitch {
		background: var(--twitch);
		color: #fff;
	}
	.kick {
		background: var(--kick);
		color: #0d1117;
	}
	.sm {
		width: 8px;
		height: 8px;
		border-radius: 2px;
	}
</style>
