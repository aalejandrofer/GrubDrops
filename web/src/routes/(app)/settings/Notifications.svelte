<script lang="ts">
	import { api } from '$lib/app';
	import { ApiError } from '$lib/api/client';
	import type { Settings, SettingsPatch } from '$lib/api/types';
	import { t } from '$lib/i18n/index.svelte';

	let {
		s,
		save,
		errs
	}: {
		s: Settings;
		save: (key: string, p: SettingsPatch) => Promise<boolean>;
		errs: Record<string, boolean>;
	} = $props();

	let editing = $state(false);
	let url = $state('');
	let busy = false;
	let feedback = $state<{ ok: boolean; text: string } | null>(null);
	let testing = $state(false);
	let input: HTMLInputElement | undefined = $state();

	const showInput = $derived(!s.discord.configured || editing);

	async function commit(): Promise<void> {
		const v = url.trim();
		if (busy) return;
		if (v === '') {
			editing = false;
			return;
		}
		busy = true;
		const ok = await save('webhook', { discord_webhook: v });
		busy = false;
		if (ok) {
			url = '';
			editing = false;
			feedback = null;
		}
	}

	function change(): void {
		editing = true;
		feedback = null;
		queueMicrotask(() => input?.focus());
	}

	async function sendTest(): Promise<void> {
		testing = true;
		feedback = null;
		try {
			await api.notifyTest();
			feedback = { ok: true, text: t('settings.test_ok') };
		} catch (e) {
			const code = e instanceof ApiError ? e.status : 0;
			feedback = {
				ok: false,
				text:
					code === 502
						? t('settings.test_rejected')
						: code === 400
							? t('settings.test_no_webhook')
							: t('settings.test_failed')
			};
		} finally {
			testing = false;
		}
	}

	const KINDS = [
		{ key: 'claim', title: 'settings.n_claim', sub: 'settings.n_claim_sub' },
		{ key: 'auth', title: 'settings.n_auth', sub: 'settings.n_auth_sub' },
		{ key: 'errors', title: 'settings.n_errors', sub: 'settings.n_errors_sub' },
		{ key: 'daily_summary', title: 'settings.n_daily', sub: 'settings.n_daily_sub' }
	] as const;
</script>

<section class="set" id="notifications" aria-labelledby="h-not">
	<div class="sec-head"><div><h2 id="h-not">{t('settings.notifications')}</h2></div></div>
	<div class="panel">
		<div class="card-body">
			<div class="card-title">
				<b>{t('settings.discord')}</b>
				{#if s.discord.configured}<span class="tag ok">{t('settings.connected')}</span>{/if}
			</div>
			<div class="field">
				<label for="dw">{t('settings.webhook_url')}</label>
				<div class="inline wrap">
					{#if showInput}
						<input
							class="inp"
							id="dw"
							type="password"
							autocomplete="off"
							spellcheck="false"
							placeholder="https://discord.com/api/webhooks/…"
							bind:value={url}
							bind:this={input}
							onblur={commit}
							onkeydown={(e) => {
								if (e.key === 'Enter') void commit();
								else if (e.key === 'Escape') {
									url = '';
									editing = false;
								}
							}}
						/>
					{:else}
						<input class="inp" id="dw" type="text" value={s.discord.webhook_hint} readonly />
						<button type="button" class="btn" disabled={testing} onclick={sendTest}>{t('settings.send_test')}</button>
						<button type="button" class="btn" onclick={change}>{t('settings.change')}</button>
					{/if}
				</div>
				{#if errs.webhook}<span class="feedback bad">{t('settings.save_failed')}</span>{/if}
				{#if feedback}<span class="feedback" class:bad={!feedback.ok} role="status">{feedback.text}</span>{/if}
			</div>
		</div>
		<div class="card-body">
			<div class="card-title">
				<b>{t('settings.telegram')}</b><span class="tag soon">{t('settings.telegram_soon')}</span>
			</div>
			<div class="two">
				<div class="field">
					<label for="tt">{t('settings.bot_token')}</label>
					<input class="inp" id="tt" type="text" placeholder="123456:ABC-DEF…" disabled />
				</div>
				<div class="field">
					<label for="tc">{t('settings.chat_id')}</label>
					<input class="inp" id="tc" type="text" placeholder="-1001234567890" disabled />
				</div>
			</div>
		</div>
	</div>

	<h3 class="sub-h">{t('settings.notify_when')}</h3>
	<div class="panel">
		{#each KINDS as k (k.key)}
			<div class="row">
				<div class="lbl">
					<b id="n-{k.key}">{t(k.title)}</b><small>{t(k.sub)}</small>
					{#if errs[`notify.${k.key}`]}<small class="bad">{t('settings.save_failed')}</small>{/if}
				</div>
				<button
					type="button"
					class="sw"
					role="switch"
					aria-checked={s.notify[k.key]}
					aria-labelledby="n-{k.key}"
					onclick={() => save(`notify.${k.key}`, { notify: { [k.key]: !s.notify[k.key] } })}
				></button>
			</div>
		{/each}
	</div>
</section>

<style>
	.bad {
		color: var(--red);
	}
</style>
