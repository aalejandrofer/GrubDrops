<script lang="ts">
	import { api } from '$lib/app';
	import type { Account } from '$lib/api/types';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { t, tn } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';
	import AccountGames from './AccountGames.svelte';

	/**
	 * Rail beside the shared list: each account's own-list switch. Turning one
	 * on opens the editor, and only Save keeps it on. Turning it off goes back
	 * to the shared list and keeps the stored list for next time.
	 */
	const live = getLive();
	const onShared = $derived(live.accounts.filter((a) => !a.own_games).length);
	const sharedGames = $derived(live.games?.games.length ?? 0);

	let editing = $state<Account | null>(null);
	let dlg = $state<HTMLDialogElement | null>(null);
	let busy = $state<string | null>(null);
	let failed = $state<string | null>(null);

	async function refresh(): Promise<void> {
		try {
			live.accounts = await api.accounts();
		} catch {
			/* the accounts event refetches */
		}
	}

	function edit(a: Account): void {
		failed = null;
		editing = a;
		dlg?.showModal();
	}

	async function toggle(a: Account): Promise<void> {
		if (!a.own_games) {
			edit(a);
			return;
		}
		busy = a.id;
		failed = null;
		try {
			await api.setAccountGames(a.id, false, null);
			await refresh();
		} catch {
			failed = a.id;
		} finally {
			busy = null;
		}
	}

	async function saved(): Promise<void> {
		dlg?.close();
		await refresh();
	}
</script>

<aside class="rail" aria-labelledby="lists-h">
	<h2 id="lists-h" class="rh">{t('games.lists')}</h2>
	<div class="ri">
		<span class="mk" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M4 6h16M4 12h16M4 18h10" /></svg></span>
		<span class="who">
			<b>{t('games.shared')}</b>
			<small>{tn('games.shared_games', sharedGames)} · {tn('games.shared_accounts', onShared)}</small>
		</span>
	</div>
	<h3 class="rh">{t('games.accounts_h')}</h3>
	<ul>
		{#each live.accounts as a (a.id)}
			<li class="ri">
				<PlatformMark platform={a.platform} />
				<span class="who">
					<b>{a.name}</b>
					{#if a.own_games}
						<small class="own"
							>{tn('games.own_count', a.own_game_count)} ·
							<button type="button" class="lnk" onclick={() => edit(a)}>{t('games.edit_list')}</button></small
						>
					{:else}
						<small>{t('games.on_shared')}</small>
					{/if}
					{#if failed === a.id}<small class="bad" role="alert">{t('accounts.games_failed')}</small>{/if}
				</span>
				<button
					type="button"
					class="sw"
					role="switch"
					aria-checked={a.own_games || editing?.id === a.id}
					aria-label={t('games.own_for', { name: a.name })}
					disabled={busy === a.id}
					onclick={() => toggle(a)}
				></button>
			</li>
		{/each}
	</ul>
</aside>

<dialog
	class="dlg"
	bind:this={dlg}
	aria-labelledby="ag-h"
	onclose={() => (editing = null)}
	onclick={(e) => e.target === dlg && dlg?.close()}
>
	{#if editing}
		<div class="dlg-head">
			<h2 id="ag-h">{t('accounts.games_title', { name: editing.name })}</h2>
			<button type="button" class="ico" aria-label={t('accounts.cancel')} onclick={() => dlg?.close()}>
				<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12" /></svg>
			</button>
		</div>
		<AccountGames account={editing} onsaved={saved} oncancel={() => dlg?.close()} />
	{/if}
</dialog>

<style>
	.rail {
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		padding: 8px;
	}
	.rh {
		margin: 8px 8px 4px;
		font-size: 11px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--t3);
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.ri {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 8px;
	}
	.mk {
		width: 14px;
		height: 14px;
		display: grid;
		place-items: center;
		color: var(--accent);
		flex: none;
	}
	.mk svg,
	.ico svg {
		width: 14px;
		height: 14px;
		fill: none;
		stroke: currentColor;
		stroke-width: 2;
		stroke-linecap: round;
	}
	.who {
		flex: 1;
		min-width: 0;
		display: grid;
		font-size: 13px;
	}
	.who b {
		font-weight: 600;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.who small {
		color: var(--t3);
		font-size: 12px;
	}
	.who small.own {
		color: var(--accent);
	}
	.who small.bad {
		color: var(--red);
	}
	.lnk {
		padding: 0;
		border: 0;
		background: none;
		color: inherit;
		font: inherit;
		text-decoration: underline;
		cursor: pointer;
	}
	.lnk:focus-visible,
	.sw:focus-visible,
	.ico:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.sw {
		position: relative;
		width: 34px;
		height: 20px;
		border-radius: 10px;
		border: 1px solid var(--border);
		background: var(--surface-3);
		flex: none;
		padding: 0;
		cursor: pointer;
		transition:
			background 0.15s,
			border-color 0.15s;
	}
	.sw::after {
		content: '';
		position: absolute;
		top: 2px;
		left: 2px;
		width: 14px;
		height: 14px;
		border-radius: 50%;
		background: var(--t2);
		transition:
			transform 0.15s,
			background 0.15s;
	}
	.sw[aria-checked='true'] {
		background: var(--accent-strong);
		border-color: var(--accent-strong);
	}
	.sw[aria-checked='true']::after {
		transform: translateX(14px);
		background: #fff;
	}
	.sw:disabled {
		opacity: 0.5;
		cursor: default;
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
	.ico {
		width: 30px;
		height: 30px;
		display: grid;
		place-items: center;
		border: 0;
		border-radius: 6px;
		background: none;
		color: var(--t2);
		cursor: pointer;
	}
	.ico:hover {
		background: var(--surface-3);
		color: var(--text);
	}
</style>
