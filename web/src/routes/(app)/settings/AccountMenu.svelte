<script lang="ts">
	import type { Account } from '$lib/api/types';
	import { menuActions } from '$lib/account-panel';
	import { t } from '$lib/i18n/index.svelte';
	import type { AccountActions } from './account-actions.svelte';

	/** A row's ⋯ menu: quick actions only. Settings and Remove live in the detail drawer. */
	let { act, a }: { act: AccountActions; a: Account } = $props();

	const open = $derived(act.menu === a.id);
	const items = $derived(menuActions(a));
</script>

<div class="menu-wrap">
	<button
		type="button"
		class="ico"
		aria-label={t('accounts.more_for', { name: a.name })}
		aria-haspopup="menu"
		aria-expanded={open}
		onclick={() => (act.menu = open ? null : a.id)}
	>
		<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12h.01M12 12h.01M19 12h.01" stroke-width="3" /></svg>
	</button>
	{#if open}
		<div class="menu" role="menu">
			{#each items as it, i (i)}
				{#if it === 'details'}
					<button type="button" role="menuitem" onclick={() => act.openPanel(a.id)}>{t('accounts.details')}</button>
					<hr />
				{:else if it === 'check'}
					<button type="button" role="menuitem" disabled={act.note(a.id, 'login')?.tone === 'wait'} onclick={() => act.check(a)}
						>{t('accounts.check_login')}</button
					>
				{:else if it === 'pause' || it === 'resume'}
					<button type="button" role="menuitem" onclick={() => act.setEnabled(a, it === 'resume')}
						>{t(it === 'pause' ? 'accounts.pause' : 'accounts.resume')}</button
					>
				{:else if it === 'restart'}
					<button type="button" role="menuitem" disabled={act.note(a.id, 'run')?.tone === 'wait'} onclick={() => act.restart(a)}
						>{t('accounts.restart')}</button
					>
				{:else}
					<button
						type="button"
						role="menuitemcheckbox"
						aria-checked={a.skip_sleep}
						title={t('accounts.skip_sleep_sub')}
						onclick={() => act.setSkipSleep(a, !a.skip_sleep)}>{t('accounts.skip_sleep')}</button
					>
				{/if}
			{/each}
		</div>
	{/if}
</div>

<style>
	.menu-wrap {
		position: relative;
	}
	.menu {
		position: absolute;
		right: 0;
		top: calc(100% + 4px);
		min-width: 190px;
		max-width: min(280px, 80vw);
		padding: 4px;
		background: var(--surface-2);
		border: 1px solid var(--border);
		border-radius: 8px;
		box-shadow: 0 10px 24px rgba(0, 0, 0, 0.4);
		z-index: 15;
	}
	.menu button {
		width: 100%;
		display: flex;
		align-items: center;
		gap: 8px;
		text-align: left;
		padding: 6px 8px;
		border: 0;
		background: none;
		border-radius: 5px;
		font: inherit;
		font-size: 13px;
		color: var(--text);
		cursor: pointer;
		white-space: nowrap;
	}
	.menu button:hover:not(:disabled) {
		background: var(--surface-3);
	}
	.menu button:disabled {
		opacity: 0.5;
		cursor: default;
	}
	.menu button:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.menu button[aria-checked='true']::after {
		content: '\2713';
		margin-left: auto;
		color: var(--accent);
	}
	.menu hr {
		margin: 4px 2px;
		border: 0;
		border-top: 1px solid var(--border);
	}
</style>
