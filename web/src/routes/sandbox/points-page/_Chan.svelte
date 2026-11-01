<script lang="ts">
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import type { Chan } from './_data';
	import type { PointsState } from './_state.svelte';

	/** Channel name with live dot, and who is on it right now. */
	let { st, c, rank, sub = true }: { st: PointsState; c: Chan; rank?: number; sub?: boolean } = $props();

	const now = $derived(c.live ? c.watching.map((id) => st.name(id)) : []);
</script>

<span class="ch">
	{#if rank !== undefined}<span class="rk">{String(rank).padStart(2, '0')}</span>{/if}
	<PlatformMark platform={c.platform} />
	<span class="who">
		<b
			><span class="nm">{c.name}</span><span
				class="dot"
				class:live={c.live}
				role="img"
				aria-label={c.live ? 'Live' : 'Offline'}
				title={c.live ? 'Live' : 'Offline'}
			></span></b
		>
		{#if sub}
			<small>
				{#if now.length}<span class="on">Watching now · {now.join(', ')}</span>
				{:else if c.live}Live · nobody on it
				{:else}Offline{/if}
			</small>
		{/if}
	</span>
</span>

<style>
	.ch {
		display: flex;
		align-items: center;
		gap: 10px;
		min-width: 0;
	}
	.rk {
		font: 500 12px/1 var(--mono);
		color: var(--t3);
		flex: none;
	}
	.who {
		display: grid;
		min-width: 0;
	}
	b {
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: 14px;
		font-weight: 600;
		min-width: 0;
	}
	.nm {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--surface-3);
		flex: none;
	}
	.dot.live {
		background: var(--red);
	}
	small {
		font-size: 12px;
		color: var(--t3);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.on {
		color: var(--green);
		font-weight: 600;
	}
</style>
