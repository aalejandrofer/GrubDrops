<script lang="ts">
	import { untrack } from 'svelte';
	import type { Platform } from '$lib/api/types';
	import type { TwitchMethod } from '$lib/accounts';
	import { t } from '$lib/i18n/index.svelte';
	import AccountFlow from './AccountFlow.svelte';
	import PlatformMark from './PlatformMark.svelte';

	/** Platform tiles, Twitch method tabs and the sign-in flow for a new account. */
	let {
		platform = 'twitch',
		id = $bindable(''),
		onadded,
		onfail,
		onpick
	}: {
		/** Platform selected first. */
		platform?: Platform;
		/** The half-made account's id once one exists. */
		id?: string;
		onadded?: () => void;
		onfail?: (msg: string) => void;
		/** A platform or method was picked. */
		onpick?: () => void;
	} = $props();

	let kind = $state<Platform>(untrack(() => platform));
	let method = $state<TwitchMethod>('android');

	function pick(p: Platform): void {
		kind = p;
		onpick?.();
	}
</script>

<div class="tiles" role="radiogroup" aria-label={t('welcome.platform')}>
	{#each ['twitch', 'kick'] as const as p (p)}
		<button type="button" class="tile" class:on={kind === p} role="radio" aria-checked={kind === p} onclick={() => pick(p)}>
			<PlatformMark platform={p} /><b>{t(p === 'twitch' ? 'accounts.twitch' : 'accounts.kick')}</b>
			<span>{t(p === 'twitch' ? 'welcome.twitch_hint' : 'welcome.kick_hint')}</span>
		</button>
	{/each}
</div>

{#if kind === 'twitch'}
	<div class="tabs2" role="tablist" aria-label={t('welcome.method')}>
		<button type="button" role="tab" aria-selected={method === 'android'} class:on={method === 'android'} onclick={() => (method = 'android')}>
			{t('welcome.android')}<span class="rec">{t('welcome.recommended')}</span>
		</button>
		<button type="button" role="tab" aria-selected={method === 'tv'} class:on={method === 'tv'} onclick={() => (method = 'tv')}>
			{t('accounts.method_tv')}
		</button>
		<button type="button" role="tab" aria-selected={method === 'tdm'} class:on={method === 'tdm'} onclick={() => (method = 'tdm')}>
			{t('accounts.method_tdm')}
		</button>
	</div>
{/if}

<div class="flowp">
	{#key `${kind}:${method}`}
		<AccountFlow {kind} start={kind === 'twitch' ? method : null} framed={false} bind:id {onadded} {onfail} />
	{/key}
</div>

<style>
	.tiles {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 10px;
	}
	.tile {
		display: grid;
		grid-template-columns: auto 1fr;
		align-items: center;
		gap: 2px 10px;
		text-align: left;
		font: inherit;
		color: var(--text);
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: 10px;
		padding: 12px 14px;
		cursor: pointer;
	}
	.tile b {
		font-weight: 500;
	}
	.tile span {
		grid-column: 2;
		font-size: 12px;
		color: var(--t3);
	}
	.tile:hover:not(.on) {
		border-color: var(--t3);
	}
	.tile.on {
		border-color: var(--accent);
		background: var(--accent-soft);
	}
	.tabs2 {
		display: flex;
		flex-wrap: wrap;
		gap: 8px 18px;
		margin: 22px 0 0;
		border-bottom: 1px solid var(--border-soft);
	}
	.tabs2 button {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		font: inherit;
		font-size: 13px;
		background: none;
		border: 0;
		color: var(--t2);
		padding: 0 0 8px;
		cursor: pointer;
	}
	.tabs2 button:hover {
		color: var(--text);
	}
	.tabs2 .on {
		color: var(--text);
		font-weight: 500;
		box-shadow: inset 0 -2px 0 var(--accent);
	}
	.tile:focus-visible,
	.tabs2 button:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.rec {
		font-size: 11px;
		font-weight: 500;
		color: var(--green);
		background: var(--green-soft);
		border-radius: 10px;
		padding: 0 7px;
	}
	.flowp {
		margin-top: 14px;
		border: 1px solid var(--border-soft);
		border-radius: 12px;
		background: var(--surface);
	}
	@media (max-width: 560px) {
		.tiles {
			grid-template-columns: 1fr;
		}
	}
</style>
