<script lang="ts">
	import type { Account } from '$lib/api/types';
	import { t } from '$lib/i18n/index.svelte';
	import type { AccountActions } from './account-actions.svelte';

	/** Danger: restart the runner, remove the account behind an inline confirm. */
	let { act, a }: { act: AccountActions; a: Account } = $props();

	const run = $derived(act.note(a.id, 'run'));
	const rm = $derived(act.note(a.id, 'remove'));
	const confirming = $derived(act.confirming === a.id);
	let removing = $state(false);

	async function remove(): Promise<void> {
		removing = true;
		await act.remove(a);
		removing = false;
	}
</script>

<section class="sec" aria-labelledby="dz-{a.id}">
	<h4 class="sh" id="dz-{a.id}">{t('accounts.danger_h')}</h4>
	<div class="opt">
		<div class="lbl">
			<b>{t('accounts.restart')}</b>
			<small>{t(a.enabled ? 'accounts.restart_sub' : 'accounts.resume_first')}</small>
			{#if run}<small class="note {run.tone}" role={run.tone === 'bad' ? 'alert' : 'status'}>{run.text}</small>{/if}
		</div>
		{#if a.enabled}
			<button type="button" class="btn" disabled={run?.tone === 'wait'} onclick={() => act.restart(a)}>
				<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M21 12a9 9 0 1 1-2.6-6.4L21 8M21 3v5h-5" /></svg>{t('accounts.restart_btn')}
			</button>
		{:else}
			<span class="chip off">{t('settings.unavailable')}</span>
		{/if}
	</div>
	<div class="opt" class:conf={confirming}>
		<div class="lbl">
			<b>{t('accounts.remove')}</b>
			{#if confirming}
				<small class="red">{t('accounts.remove_confirm', { name: a.name })}</small>
			{:else}
				<small>{t('accounts.remove_sub')}</small>
			{/if}
			{#if rm}<small class="note bad" role="alert">{rm.text}</small>{/if}
		</div>
		{#if confirming}
			<div class="acts">
				<button type="button" class="btn danger solid" disabled={removing} onclick={remove}>{t('accounts.remove_yes')}</button>
				<button type="button" class="btn" onclick={() => (act.confirming = null)}>{t('accounts.cancel')}</button>
			</div>
		{:else}
			<button type="button" class="btn danger" onclick={() => (act.confirming = a.id)}>{t('accounts.remove_ask')}</button>
		{/if}
	</div>
</section>

<style>
	.conf {
		padding: 10px;
		margin: -4px -10px;
		border-radius: 8px;
		background: var(--red-soft);
	}
	.red {
		color: color-mix(in srgb, var(--red) 70%, var(--text)) !important;
	}
	.btn.danger {
		color: var(--red);
	}
	.btn.danger.solid {
		background: var(--red);
		border-color: var(--red);
		color: #fff;
	}
</style>
