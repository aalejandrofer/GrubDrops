<script lang="ts">
	import { MODES, MODE_SUB, type Acct } from './_data';
	import type { PointsState } from './_state.svelte';

	/** Off / Drops + points / Points only, as a segmented control. */
	let { st, a, short = false }: { st: PointsState; a: Acct; short?: boolean } = $props();
</script>

<div class="seg" role="radiogroup" aria-label="Channel points for {a.name}" title={a.mode ? MODE_SUB[a.mode] : ''}>
	{#each MODES as m (m.mode)}
		<button
			type="button"
			role="radio"
			aria-checked={a.mode === m.mode}
			title={MODE_SUB[m.mode]}
			onclick={() => st.setMode(a.id, m.mode)}>{short ? m.short : m.label}</button
		>
	{/each}
</div>

<style>
	.seg {
		display: grid;
		grid-template-columns: repeat(3, auto);
		border: 1px solid var(--border);
		border-radius: 8px;
		overflow: hidden;
		min-width: 0;
	}
	button {
		font: inherit;
		font-size: 12px;
		padding: 0 8px;
		height: 26px;
		min-width: 0;
		border: 0;
		background: var(--surface);
		color: var(--t2);
		cursor: pointer;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	button + button {
		border-left: 1px solid var(--border);
	}
	button:hover {
		color: var(--text);
	}
	button[aria-checked='true'] {
		background: var(--accent-soft);
		color: var(--accent);
		font-weight: 600;
	}
</style>
