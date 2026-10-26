<script lang="ts">
	import { api } from '$lib/app';
	import { recheckAccounts, type RecheckResult } from '$lib/games';
	import { t, tn } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';
	import Icon from './Icon.svelte';

	let { accountIds }: { accountIds: string[] } = $props();

	const live = getLive();
	let busy = $state(false);
	let result = $state<RecheckResult | null>(null);

	async function check(): Promise<void> {
		busy = true;
		result = null;
		try {
			result = await recheckAccounts(accountIds, (id) => api.recheckLinks(id));
			await live?.refetchAll();
		} finally {
			busy = false;
		}
	}

	const text = $derived(
		result === null
			? ''
			: result.outcome === 'claimed'
				? tn('games.recheck_claimed', result.claimed)
				: result.outcome === 'still'
					? t('games.recheck_still')
					: result.outcome === 'failed'
						? t('games.recheck_failed')
						: t('games.recheck_linked')
	);
</script>

<button type="button" class="recheck" disabled={busy} aria-busy={busy} onclick={check}>
	<span class="ic" class:spin={busy}><Icon name="refresh" size={12} /></span>{busy ? t('games.rechecking') : t('games.recheck')}
</button>
{#if text}<span class="res" class:ok={result?.outcome === 'claimed' || result?.outcome === 'linked'} role="status">{text}</span>{/if}

<style>
	.recheck {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		background: none;
		border: 1px solid var(--border);
		border-radius: 6px;
		padding: 1px 7px;
		font: inherit;
		font-size: 12px;
		color: var(--t2);
		cursor: pointer;
		white-space: nowrap;
	}
	.recheck:hover:not(:disabled) {
		color: var(--text);
		border-color: var(--t2);
	}
	.recheck:disabled {
		cursor: default;
		opacity: 0.7;
	}
	.recheck:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.ic {
		display: inline-flex;
	}
	.spin {
		animation: spin 0.9s linear infinite;
	}
	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.spin {
			animation: none;
		}
	}
	.res {
		font-size: 12px;
		color: var(--t2);
	}
	.res.ok {
		color: var(--green);
	}
</style>
