<script lang="ts">
	import type { PointsState } from './_state.svelte';

	/** Up / drag handle / down, as on the Games list. */
	let { st, i, n, row = false }: { st: PointsState; i: number; n: number; row?: boolean } = $props();
</script>

<div class="grip" class:row>
	<button type="button" aria-label="Move up" title="Move up" disabled={i === 0} onclick={() => st.move(i, i - 1)}>
		<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 15 6-6 6 6" /></svg>
	</button>
	<span class="h" title="Drag to reorder" role="presentation">
		<svg class="i" viewBox="0 0 24 24" aria-hidden="true"
			><path d="M9 6h.01M15 6h.01M9 12h.01M15 12h.01M9 18h.01M15 18h.01" stroke-width="3" /></svg
		>
	</span>
	<button type="button" aria-label="Move down" title="Move down" disabled={i === n - 1} onclick={() => st.move(i, i + 1)}>
		<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg>
	</button>
</div>

<style>
	.grip {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 2px;
		color: var(--t3);
		flex: none;
	}
	.grip.row {
		flex-direction: row;
		gap: 0;
	}
	.h {
		cursor: grab;
		padding: 2px 0;
		touch-action: none;
		display: grid;
	}
	button {
		width: 26px;
		height: 22px;
		display: grid;
		place-items: center;
		border: 0;
		background: none;
		border-radius: 4px;
		color: var(--t2);
		cursor: pointer;
	}
	button:hover:not(:disabled) {
		background: var(--surface-3);
		color: var(--text);
	}
	button:disabled {
		opacity: 0.3;
		cursor: default;
	}
	.i {
		width: 14px;
		height: 14px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.8;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
</style>
