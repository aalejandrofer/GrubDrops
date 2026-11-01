<script lang="ts">
	import type { Account } from '$lib/api/types';
	import AccountFlow from '$lib/components/AccountFlow.svelte';
	import { relTime } from '$lib/format';
	import { t } from '$lib/i18n/index.svelte';
	import type { AccountActions } from './account-actions.svelte';

	/** Login: session health, check now, and the sign-in flow to log in again. */
	let { act, a, busy = $bindable(false) }: { act: AccountActions; a: Account; busy?: boolean } = $props();

	let now = $state(Date.now());
	$effect(() => {
		const h = setInterval(() => (now = Date.now()), 30_000);
		return () => clearInterval(h);
	});

	const flowing = $derived(act.relogin === a.id);
	const n = $derived(act.note(a.id, 'login'));
	const checked = $derived(a.auth?.checked_at ? t('accounts.checked', { time: relTime(a.auth.checked_at, now) }) : '');
	let failed = $state('');
</script>

<section class="sec" aria-labelledby="lg-{a.id}">
	<h4 class="sh" id="lg-{a.id}">{t('accounts.login_h')}</h4>
	<dl class="kv">
		<dt>{t('accounts.platform_login')}</dt>
		<dd class="mono">@{a.platform_name}</dd>
		<dt>{t('accounts.session')}</dt>
		<dd>
			{#if a.needs_login}
				<span class="bad">{t('accounts.session_bad')}</span>{#if a.auth?.detail}
					· {a.auth.detail}{/if}{#if checked}
					· {checked}{/if}
			{:else if a.auth}
				<span class={a.auth.ok ? 'good' : 'bad'}>{t(a.auth.ok ? 'accounts.session_ok' : 'accounts.session_bad')}</span>{#if checked}
					· {checked}{/if}
			{:else}
				{t('accounts.session_unchecked')}
			{/if}
		</dd>
	</dl>
	{#if a.needs_login && !flowing}
		<p class="warn">{t('accounts.signed_out_warn')}</p>
	{/if}

	{#if flowing}
		<div class="flow-box">
			<AccountFlow
				bind:busy
				kind={a.platform}
				existing={a.id}
				onclose={() => (act.relogin = null)}
				ondone={() => act.loggedIn(a)}
				onfail={(msg) => {
					failed = msg;
					act.relogin = null;
				}}
			/>
		</div>
	{:else}
		<div class="acts">
			<button type="button" class="btn" disabled={n?.tone === 'wait'} onclick={() => act.check(a)}>{t('accounts.check_login')}</button>
			<button
				type="button"
				class="btn"
				class:primary={a.needs_login}
				disabled={busy}
				onclick={() => {
					failed = '';
					act.relogin = a.id;
				}}>{t('accounts.log_in_again')}</button
			>
		</div>
	{/if}
	{#if failed}<p class="note bad" role="alert">{failed}</p>{/if}
	{#if n}<p class="note {n.tone}" role={n.tone === 'bad' ? 'alert' : 'status'}>{n.text}</p>{/if}
</section>

<style>
	.good {
		color: var(--green);
		font-weight: 500;
	}
	.bad {
		color: var(--red);
		font-weight: 500;
	}
	.warn {
		margin: 0;
		font-size: 12.5px;
		padding: 8px 10px;
		border-radius: 7px;
		background: var(--red-soft);
		color: color-mix(in srgb, var(--red) 70%, var(--text));
	}
	.flow-box {
		border: 1px solid var(--border-soft);
		border-radius: 8px;
		background: var(--surface);
		min-width: 0;
	}
</style>
