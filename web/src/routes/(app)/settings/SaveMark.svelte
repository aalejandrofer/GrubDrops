<script lang="ts">
	import { t } from '$lib/i18n/index.svelte';
	import type { Mark } from './account-actions.svelte';

	/** "Saving…", then "Saved" that fades, or "Not saved" until the next try. */
	let { mark }: { mark: Mark | undefined } = $props();
</script>

{#if mark}
	<span class="sv {mark}" role="status">
		{#if mark === 'ok'}
			<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="m5 12.5 4.5 4.5L19 7.5" /></svg>{t('accounts.saved')}
		{:else if mark === 'bad'}
			{t('accounts.not_saved')}
		{:else}
			{t('accounts.saving_now')}
		{/if}
	</span>
{/if}

<style>
	.sv {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		margin-left: 6px;
		font-size: 12px;
		font-weight: 500;
		color: var(--t3);
		white-space: nowrap;
		vertical-align: middle;
	}
	.sv.ok {
		color: var(--green);
	}
	.sv.bad {
		color: var(--red);
	}
	.i {
		width: 12px;
		height: 12px;
		fill: none;
		stroke: currentColor;
		stroke-width: 2;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
</style>
