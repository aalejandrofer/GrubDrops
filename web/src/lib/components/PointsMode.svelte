<script lang="ts">
	import type { Account, PointsMode } from '$lib/api/types';
	import { POINTS_CHOICES } from '$lib/accounts';
	import { t } from '$lib/i18n/index.svelte';

	/** Drops only / Drops + points / Points only, as a segmented control. */
	let { a, onpick }: { a: Account; onpick: (mode: PointsMode) => void } = $props();
</script>

<div class="pm" role="radiogroup" aria-label={t('points.mode_for', { name: a.name })} title={t('accounts.points_sub')}>
	{#each POINTS_CHOICES as c (c.mode)}
		<button
			type="button"
			role="radio"
			aria-checked={a.points_mode === c.mode}
			onclick={() => a.points_mode !== c.mode && onpick(c.mode)}>{t(c.label)}</button
		>
	{/each}
</div>

<style>
	.pm {
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
	button:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
</style>
