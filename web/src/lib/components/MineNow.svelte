<script lang="ts">
	import { api } from '$lib/app';
	import { t } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';
	import { commitMineNow, mineTarget, queues } from '$lib/upnext';

	// Starts the first Up next drop of the first account that can switch. The
	// runner switches on its own goroutine; its runner event carries the result.
	let { onresult }: { onresult: (ok: boolean) => void } = $props();
	const live = getLive();
	const target = $derived(mineTarget(queues(live.runners)));
	let busy = $state(false);

	async function go(): Promise<void> {
		if (busy || !target) return;
		busy = true;
		const ok = await commitMineNow(
			{
				get: () => live.runners,
				set: (r) => (live.runners = r),
				mineNow: (id) => api.mineNow(id),
				fetchRunners: () => api.runners()
			},
			target.accountId
		);
		busy = false;
		onresult(ok);
	}
</script>

{#if target}
	<button
		class="mine"
		type="button"
		disabled={busy}
		aria-busy={busy}
		title={t('upnext.mine_now_title', { drop: target.items[0].benefit_name })}
		onclick={go}
	>
		{t('upnext.mine_now')}
	</button>
{/if}

<style>
	.mine {
		margin-left: auto;
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
