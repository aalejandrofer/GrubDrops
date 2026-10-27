<script lang="ts">
	import { api } from '$lib/app';
	import { ApiError } from '$lib/api/client';
	import type { Settings, SettingsPatch } from '$lib/api/types';
	import { t } from '$lib/i18n/index.svelte';
	import { isAvatarURL, isDiscordWebhook, isTelegramChat, isTelegramToken } from '$lib/settings';

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
	let invalid = $state(false);
	let testing = $state(false);
	let input: HTMLInputElement | undefined = $state();

	const showInput = $derived(!s.discord.configured || editing);

	// Saves only on the Save button or Enter, never on blur or input, so a
	// browser autofill can't send anything.
	async function commit(): Promise<void> {
		const v = url.trim();
		if (busy || v === '') return;
		if (!isDiscordWebhook(v)) {
			invalid = true;
			return;
		}
		invalid = false;
		busy = true;
		const ok = await save('webhook', { discord_webhook: v });
		busy = false;
		if (ok) {
			url = '';
			editing = false;
			feedback = null;
		}
	}

	function cancel(): void {
		url = '';
		invalid = false;
		editing = false;
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
			await api.notifyTest('discord');
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

	let tgEditing = $state(false);
	let tgToken = $state('');
	let tgChat = $state('');
	let tgBusy = false;
	let tgTokenBad = $state(false);
	let tgChatBad = $state(false);
	let tgTesting = $state(false);
	let tgFeedback = $state<{ ok: boolean; text: string } | null>(null);
	let tgInput: HTMLInputElement | undefined = $state();

	const tgShowInput = $derived(!s.telegram.configured || tgEditing);
	// A configured bot can change its chat ID without re-entering the token.
	const tgCanSave = $derived(tgChat.trim() !== '' && (tgToken.trim() !== '' || s.telegram.configured));

	// Same rule as the Discord field: saves only on Save or Enter.
	async function tgCommit(): Promise<void> {
		const tok = tgToken.trim();
		const chat = tgChat.trim();
		if (tgBusy || !tgCanSave) return;
		tgTokenBad = tok !== '' && !isTelegramToken(tok);
		tgChatBad = !isTelegramChat(chat);
		if (tgTokenBad || tgChatBad) return;
		tgBusy = true;
		const ok = await save('telegram', tok ? { telegram_bot_token: tok, telegram_chat_id: chat } : { telegram_chat_id: chat });
		tgBusy = false;
		if (ok) {
			tgToken = '';
			tgChat = '';
			tgEditing = false;
			tgFeedback = null;
		}
	}

	function tgCancel(): void {
		tgToken = '';
		tgChat = '';
		tgTokenBad = false;
		tgChatBad = false;
		tgEditing = false;
	}

	function tgChange(): void {
		tgEditing = true;
		tgChat = s.telegram.chat_id;
		tgFeedback = null;
		queueMicrotask(() => tgInput?.focus());
	}

	function tgKey(e: KeyboardEvent): void {
		if (e.key === 'Enter' && e.isTrusted) void tgCommit();
		else if (e.key === 'Escape') tgCancel();
	}

	async function tgSendTest(): Promise<void> {
		tgTesting = true;
		tgFeedback = null;
		try {
			await api.notifyTest('telegram');
			tgFeedback = { ok: true, text: t('settings.test_ok_telegram') };
		} catch (e) {
			const code = e instanceof ApiError ? e.status : 0;
			tgFeedback = {
				ok: false,
				text:
					code === 502
						? t('settings.test_rejected_telegram')
						: code === 400
							? t('settings.test_no_telegram')
							: t('settings.test_failed')
			};
		} finally {
			tgTesting = false;
		}
	}

	const KINDS = [
		{ key: 'claim', title: 'settings.n_claim', sub: 'settings.n_claim_sub' },
		{ key: 'auth', title: 'settings.n_auth', sub: 'settings.n_auth_sub' },
		{ key: 'errors', title: 'settings.n_errors', sub: 'settings.n_errors_sub' },
		{ key: 'daily_summary', title: 'settings.n_daily', sub: 'settings.n_daily_sub' }
	] as const;
	const ADV_KINDS = [
		{ key: 'progress', title: 'settings.n_progress', sub: 'settings.n_progress_sub' },
		{ key: 'canary', title: 'settings.n_canary', sub: 'settings.n_canary_sub' }
	] as const;

	let avatar = $state('');
	let avatarDirty = $state(false);
	let avatarBad = $state(false);
	$effect(() => {
		if (!avatarDirty) avatar = s.notify_avatar_url;
	});

	async function saveAvatar(): Promise<void> {
		const v = avatar.trim();
		if (!isAvatarURL(v)) {
			avatarBad = true;
			return;
		}
		if (await save('notify_avatar_url', { notify_avatar_url: v })) avatarDirty = false;
	}
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
				<label for="discord-webhook">{t('settings.webhook_url')}</label>
				<div class="inline wrap">
					{#if showInput}
						<input
							class="inp masked"
							id="discord-webhook"
							name="discord-webhook"
							type="url"
							inputmode="url"
							autocomplete="off"
							spellcheck="false"
							data-1p-ignore
							data-lpignore="true"
							data-bwignore
							placeholder="https://discord.com/api/webhooks/…"
							aria-invalid={invalid ? true : undefined}
							bind:value={url}
							bind:this={input}
							oninput={() => (invalid = false)}
							onkeydown={(e) => {
								if (e.key === 'Enter' && e.isTrusted) void commit();
								else if (e.key === 'Escape') cancel();
							}}
						/>
						<button type="button" class="btn" disabled={url.trim() === ''} onclick={commit}>{t('accounts.save')}</button>
						{#if s.discord.configured}
							<button type="button" class="btn" onclick={cancel}>{t('accounts.cancel')}</button>
						{/if}
					{:else}
						<input class="inp" id="discord-webhook" type="text" value={s.discord.webhook_hint} readonly />
						<button type="button" class="btn" onclick={change}>{t('settings.change')}</button>
					{/if}
				</div>
				{#if invalid}<span class="feedback bad" role="alert">{t('settings.webhook_invalid')}</span>{/if}
				{#if errs.webhook}<span class="feedback bad">{t('settings.save_failed')}</span>{/if}
			</div>
		</div>
		<div class="card-body">
			<div class="card-title">
				<b>{t('settings.telegram')}</b>
				{#if s.telegram.configured}<span class="tag ok">{t('settings.connected')}</span>{/if}
			</div>
			{#if tgShowInput}
				<div class="two">
					<div class="field">
						<label for="tg-token">{t('settings.bot_token')}</label>
						<input
							class="inp masked"
							id="tg-token"
							name="tg-token"
							type="text"
							autocomplete="off"
							spellcheck="false"
							data-1p-ignore
							data-lpignore="true"
							data-bwignore
							placeholder={s.telegram.configured ? t('settings.token_keep') : '123456:ABC-DEF…'}
							aria-invalid={tgTokenBad ? true : undefined}
							bind:value={tgToken}
							bind:this={tgInput}
							oninput={() => (tgTokenBad = false)}
							onkeydown={tgKey}
						/>
					</div>
					<div class="field">
						<label for="tg-chat">{t('settings.chat_id')}</label>
						<input
							class="inp"
							id="tg-chat"
							name="tg-chat"
							type="text"
							autocomplete="off"
							spellcheck="false"
							placeholder="-1001234567890"
							aria-invalid={tgChatBad ? true : undefined}
							bind:value={tgChat}
							oninput={() => (tgChatBad = false)}
							onkeydown={tgKey}
						/>
					</div>
				</div>
				<div class="inline wrap">
					<button type="button" class="btn" disabled={!tgCanSave} onclick={tgCommit}>{t('accounts.save')}</button>
					{#if s.telegram.configured}
						<button type="button" class="btn" onclick={tgCancel}>{t('accounts.cancel')}</button>
					{/if}
				</div>
			{:else}
				<div class="two">
					<div class="field">
						<label for="tg-token">{t('settings.bot_token')}</label>
						<input class="inp" id="tg-token" type="text" value={s.telegram.token_hint} readonly />
					</div>
					<div class="field">
						<label for="tg-chat">{t('settings.chat_id')}</label>
						<input class="inp" id="tg-chat" type="text" value={s.telegram.chat_id} readonly />
					</div>
				</div>
				<div class="inline wrap">
					<button type="button" class="btn" onclick={tgChange}>{t('settings.change')}</button>
				</div>
			{/if}
			<small class="help">{t('settings.telegram_help')}</small>
			{#if tgTokenBad}<span class="feedback bad" role="alert">{t('settings.token_invalid')}</span>{/if}
			{#if tgChatBad}<span class="feedback bad" role="alert">{t('settings.chat_invalid')}</span>{/if}
			{#if errs.telegram}<span class="feedback bad">{t('settings.save_failed')}</span>{/if}
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

	<div class="panel test">
		<div class="row">
			<div class="lbl">
				<b>{t('settings.test_notify')}</b>
				<small>{s.discord.configured || s.telegram.configured ? t('settings.test_notify_sub') : t('settings.test_notify_none')}</small>
				{#if feedback}<small class:bad={!feedback.ok} role="status">{feedback.text}</small>{/if}
				{#if tgFeedback}<small class:bad={!tgFeedback.ok} role="status">{tgFeedback.text}</small>{/if}
			</div>
			<div class="inline wrap">
				{#if s.discord.configured}
					<button type="button" class="btn" disabled={testing} onclick={sendTest}>{t('settings.test_discord')}</button>
				{/if}
				{#if s.telegram.configured}
					<button type="button" class="btn" disabled={tgTesting} onclick={tgSendTest}>{t('settings.test_telegram')}</button>
				{/if}
			</div>
		</div>
	</div>

	<details class="adv">
		<summary>{t('settings.advanced')}</summary>
		<div class="panel">
			{#each ADV_KINDS as k (k.key)}
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
			<div class="card-body">
				<div class="field">
					<label for="avatar-url">{t('settings.avatar_url')}</label>
					<div class="inline wrap">
						<input
							class="inp grow"
							id="avatar-url"
							name="avatar-url"
							type="url"
							inputmode="url"
							autocomplete="off"
							spellcheck="false"
							placeholder="https://…/avatar.png"
							aria-invalid={avatarBad ? true : undefined}
							bind:value={avatar}
							oninput={() => ((avatarDirty = true), (avatarBad = false))}
							onkeydown={(e) => {
								if (e.key === 'Enter' && e.isTrusted && avatarDirty) void saveAvatar();
							}}
						/>
						<button type="button" class="btn" disabled={!avatarDirty} onclick={saveAvatar}>{t('accounts.save')}</button>
					</div>
					<small class="help">{t('settings.avatar_help')}</small>
					{#if avatarBad}<span class="feedback bad" role="alert">{t('settings.avatar_invalid')}</span>{/if}
					{#if errs.notify_avatar_url}<span class="feedback bad">{t('settings.save_failed')}</span>{/if}
				</div>
			</div>
		</div>
	</details>
</section>

<style>
	/* Masked by CSS, not a password input, so password managers leave it alone. */
	.masked {
		-webkit-text-security: disc;
	}
	.bad {
		color: var(--red);
	}
	.help {
		color: var(--t2);
		font-size: 12px;
	}
	.test {
		margin-top: 14px;
	}
	.grow {
		flex: 1;
		min-width: 0;
	}
</style>
