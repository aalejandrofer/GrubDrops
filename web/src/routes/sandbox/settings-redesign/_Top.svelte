<script lang="ts">
	import type { Snippet } from 'svelte';
	import Ico from './_Ico.svelte';
	import { st } from './_state.svelte';

	/** Stand-in for the app page head: title, autosave stamp, a quiet Saved mark. */
	let { stamp = 'Saves automatically', children }: { stamp?: string; children?: Snippet } = $props();
</script>

<header class="top">
	<h1>Settings</h1>
	<span class="stamp">{stamp}</span>
	{#if st.saved}<span class="saved"><Ico name="check" size={13} /> {st.saved}</span>{/if}
	{#if children}<div class="extra">{@render children()}</div>{/if}
</header>

<style>
	.top {
		display: flex;
		align-items: baseline;
		gap: 12px;
		flex-wrap: wrap;
		margin-bottom: 22px;
	}
	h1 {
		margin: 0;
		font-size: 22px;
		font-weight: 600;
		letter-spacing: -0.02em;
	}
	.stamp {
		font-size: 12.5px;
		color: var(--t3);
	}
	.saved {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		font-size: 12.5px;
		color: var(--green);
	}
	.extra {
		margin-left: auto;
		align-self: center;
	}
</style>
