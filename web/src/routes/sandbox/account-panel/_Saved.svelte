<script lang="ts">
	import type { PanelState } from './_state.svelte';

	/** "Saving…" then "Saved" for one account field, fades on its own. */
	let { st, k }: { st: PanelState; k: string } = $props();
	const s = $derived(st.saved[k]);
</script>

{#if s}
	<span class="sv {s}" role="status">
		{#if s === 'ok'}
			<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="m5 12.5 4.5 4.5L19 7.5" /></svg>Saved
		{:else}Saving…{/if}
	</span>
{/if}

<style>
	.sv {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		font-size: 12px;
		font-weight: 500;
		color: var(--t3);
		white-space: nowrap;
	}
	.sv.ok {
		color: var(--green);
	}
</style>
