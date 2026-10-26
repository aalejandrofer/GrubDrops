<script lang="ts">
	import { api } from '$lib/app';
	import { t } from '$lib/i18n/index.svelte';

	// The runner switches on its own goroutine; the live store picks the new
	// current drop up from the runner event.
	let {
		accountId,
		benefitId,
		onresult
	}: { accountId: string; benefitId: string; onresult: (ok: boolean) => void } = $props();
	let busy = $state(false);

	async function go(): Promise<void> {
		if (busy) return;
		busy = true;
		try {
			await api.mineNow(accountId, benefitId);
			onresult(true);
		} catch {
			onresult(false);
		} finally {
			busy = false;
		}
	}
</script>

<button class="mine" type="button" disabled={busy} aria-busy={busy} title={t('upnext.mine_now_title')} onclick={go}>
	{t('upnext.mine_now')}
</button>

<style>
	.mine {
		font-size: 12px;
		color: var(--t2);
		background: none;
		border: 1px solid var(--border);
		border-radius: 6px;
		padding: 2px 8px;
		white-space: nowrap;
		cursor: pointer;
	}
	.mine:hover:not(:disabled) {
		color: var(--text);
		border-color: var(--t2);
	}
	.mine:disabled {
		opacity: 0.5;
		cursor: progress;
	}
</style>
