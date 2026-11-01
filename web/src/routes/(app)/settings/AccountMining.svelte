<script lang="ts">
	import NewPill from '$lib/components/NewPill.svelte';
	import type { Account, Runner } from '$lib/api/types';
	import { fieldKey, mineBlock, nextDrop } from '$lib/account-panel';
	import { t, tn } from '$lib/i18n/index.svelte';
	import AccountGames from '../games/AccountGames.svelte';
	import type { AccountActions } from './account-actions.svelte';
	import SaveMark from './SaveMark.svelte';

	/** Mining: on or paused, own game list, skip sleep window, Mine now. */
	let { act, a, r }: { act: AccountActions; a: Account; r: Runner | undefined } = $props();

	const next = $derived(nextDrop(r));
	const why = $derived(mineBlock(a, r));
	const mine = $derived(act.note(a.id, 'mine'));
	const errs = $derived(
		(['enabled', 'own', 'skip'] as const).flatMap((f) => {
			const n = act.note(a.id, f);
			return n ? [{ f, text: n.text }] : [];
		})
	);

	let dlg = $state<HTMLDialogElement | null>(null);
	let editing = $state(false);

	function edit(): void {
		editing = true;
		dlg?.showModal();
	}

	// Turning the list on opens the editor; only its Save keeps it on.
	function toggleOwn(): void {
		if (a.own_games) void act.ownOff(a);
		else edit();
	}
</script>

<section class="sec" aria-labelledby="mn-{a.id}">
	<h4 class="sh" id="mn-{a.id}">{t('accounts.mining_h')}</h4>
	<div class="opt">
		<div class="lbl">
			<b id="en-{a.id}">{t('accounts.mining')}<SaveMark mark={act.marks[fieldKey(a.id, 'enabled')]} /></b>
			<small>{t(a.enabled ? 'accounts.mining_on' : 'accounts.mining_off')}</small>
		</div>
		<button
			type="button"
			class="sw"
			role="switch"
			aria-checked={a.enabled}
			aria-labelledby="en-{a.id}"
			disabled={act.marks[fieldKey(a.id, 'enabled')] === 'wait'}
			onclick={() => act.setEnabled(a, !a.enabled)}
		></button>
	</div>
	<div class="opt">
		<div class="lbl">
			<b id="og-{a.id}">{t('accounts.games_own_on')}<SaveMark mark={act.marks[fieldKey(a.id, 'own')]} /></b>
			{#if a.own_games}
				<small
					>{tn('games.own_count', a.own_game_count)} ·
					<button type="button" class="lnk" onclick={edit}>{t('games.edit_list')}</button></small
				>
			{:else}
				<small>{t('accounts.games_shared_sub')}</small>
			{/if}
		</div>
		<button
			type="button"
			class="sw"
			role="switch"
			aria-checked={a.own_games || editing}
			aria-labelledby="og-{a.id}"
			disabled={act.marks[fieldKey(a.id, 'own')] === 'wait'}
			onclick={toggleOwn}
		></button>
	</div>
	<div class="opt">
		<div class="lbl">
			<b id="sk-{a.id}">{t('accounts.skip_sleep')}<NewPill since="2.3" /><SaveMark mark={act.marks[fieldKey(a.id, 'skip')]} /></b>
			<small>{t('accounts.skip_sleep_sub')}</small>
		</div>
		<button
			type="button"
			class="sw"
			role="switch"
			aria-checked={a.skip_sleep}
			aria-labelledby="sk-{a.id}"
			disabled={act.marks[fieldKey(a.id, 'skip')] === 'wait'}
			onclick={() => act.setSkipSleep(a, !a.skip_sleep)}
		></button>
	</div>
	{#each errs as e (e.f)}<p class="note bad" role="alert">{e.text}</p>{/each}
	<div class="opt">
		<div class="lbl">
			<b>{t('accounts.up_next')}</b>
			{#if next}
				<small>{t('accounts.next_line', { game: next.game, drop: next.benefit_name, m: next.minutes, req: next.required_minutes })}</small>
				{#if why}<small class="dim">{t(why)}</small>{/if}
			{:else}
				<small>{t('accounts.next_none')}</small>
			{/if}
			{#if mine}<small class="note {mine.tone}" role={mine.tone === 'bad' ? 'alert' : 'status'}>{mine.text}</small>{/if}
		</div>
		{#if why}
			<span class="chip off">{t('settings.unavailable')}</span>
		{:else}
			<button type="button" class="btn" disabled={mine?.tone === 'wait'} onclick={() => act.mineNow(a)}>
				<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M7 5v14l11-7z" /></svg>{t('accounts.mine_now')}
			</button>
		{/if}
	</div>
</section>

<dialog class="dlg" bind:this={dlg} aria-labelledby="ag-{a.id}" onclose={() => (editing = false)} onclick={(e) => e.target === dlg && dlg?.close()}>
	{#if editing}
		<div class="dlg-head">
			<h2 id="ag-{a.id}">{t('accounts.games_title', { name: a.name })}</h2>
			<button type="button" class="ico" aria-label={t('accounts.cancel')} onclick={() => dlg?.close()}>
				<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12" /></svg>
			</button>
		</div>
		<AccountGames
			account={a}
			onsaved={() => {
				dlg?.close();
				void act.ownSaved(a);
			}}
			oncancel={() => dlg?.close()}
		/>
	{/if}
</dialog>

<style>
	.dim {
		color: var(--t3) !important;
	}
	.dlg {
		width: min(520px, calc(100vw - 32px));
		max-height: calc(100dvh - 32px);
		padding: 20px;
		border: 1px solid var(--border);
		border-radius: 12px;
		background: var(--bg);
		color: var(--text);
	}
	.dlg::backdrop {
		background: rgb(0 0 0 / 0.55);
	}
	.dlg-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		margin-bottom: 10px;
	}
	.dlg-head h2 {
		margin: 0;
		font-size: 17px;
		font-weight: 600;
	}
</style>
