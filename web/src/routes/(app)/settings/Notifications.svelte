<script lang="ts">
	import NewPill from '$lib/components/NewPill.svelte';
	import { SECTION_LOOK } from '$lib/settings-look';
	import { api } from '$lib/app';
	import { ApiError } from '$lib/api/client';
	import type { Settings, SettingsPatch } from '$lib/api/types';
	import { typed, type Autosave } from '$lib/autosave';
	import { t } from '$lib/i18n/index.svelte';
	import { isAvatarURL, isDiscordWebhook, isMentionID, isTelegramChat, isTelegramToken } from '$lib/settings';
	import Icon from '$lib/components/Icon.svelte';
	import SectionHead from './SectionHead.svelte';

	let {
		s,
		save,
		errs,
		auto
	}: {
		s: Settings;
		save: (key: string, p: SettingsPatch) => Promise<boolean>;
		errs: Record<string, boolean>;
		auto: Autosave;
	} = $props();

	let editing = $state(false);
	let url = $state('');
	let busy = false;
	let feedback = $state<{ ok: boolean; text: string } | null>(null);
	let invalid = $state(false);
	let testing = $state(false);
	let input: HTMLInputElement | undefined = $state();

	const showInput = $derived(!s.discord.configured || editing);

	// Saves 10s after typing stops, or on Save or Enter. Only typing or pasting
	// starts the countdown, so a browser autofill can't send anything.
	async function commit(): Promise<boolean> {
		const v = url.trim();
		if (busy || v === '') return false;
		if (!isDiscordWebhook(v)) {
			invalid = true;
			return false;
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
		return ok;
	}
	const hookValid = (): boolean => isDiscordWebhook(url.trim());
	const commitNow = (): Promise<boolean> => auto.now('webhook', commit, hookValid);

	function editHook(e: Event): void {
		invalid = false;
		if (url.trim() === '') auto.drop('webhook');
		else if (typed(e as InputEvent)) auto.edit('webhook', commit, hookValid);
	}

	function cancel(): void {
		auto.drop('webhook');
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

	// The Telegram form opens on Connect, or on Change once a bot is set up.
	let tgEditing = $state(false);
	let tgToken = $state('');
	let tgChat = $state('');
	let tgBusy = false;
	let tgTokenBad = $state(false);
	let tgChatBad = $state(false);
	let tgTesting = $state(false);
	let tgFeedback = $state<{ ok: boolean; text: string } | null>(null);
	let tgInput: HTMLInputElement | undefined = $state();

	// A configured bot can change its chat ID without re-entering the token.
	const tgCanSave = $derived(tgChat.trim() !== '' && (tgToken.trim() !== '' || s.telegram.configured));

	// Same rule as the Discord field.
	async function tgCommit(): Promise<boolean> {
		const tok = tgToken.trim();
		const chat = tgChat.trim();
		if (tgBusy) return false;
		if (!tgCanSave) {
			tgChatBad = chat === '' && tok !== '';
			return false;
		}
		tgTokenBad = tok !== '' && !isTelegramToken(tok);
		tgChatBad = !isTelegramChat(chat);
		if (tgTokenBad || tgChatBad) return false;
		tgBusy = true;
		const ok = await save('telegram', tok ? { telegram_bot_token: tok, telegram_chat_id: chat } : { telegram_chat_id: chat });
		tgBusy = false;
		if (ok) {
			tgToken = '';
			tgChat = '';
			tgEditing = false;
			tgFeedback = null;
		}
		return ok;
	}
	function tgValid(): boolean {
		const tok = tgToken.trim();
		return tgCanSave && (tok === '' || isTelegramToken(tok)) && isTelegramChat(tgChat.trim());
	}
	const tgCommitNow = (): Promise<boolean> => auto.now('telegram', tgCommit, tgValid);

	function tgEdit(e: Event): void {
		tgTokenBad = false;
		tgChatBad = false;
		if (tgToken.trim() === '' && tgChat.trim() === '') auto.drop('telegram');
		else if (typed(e as InputEvent)) auto.edit('telegram', tgCommit, tgValid);
	}

	function tgCancel(): void {
		auto.drop('telegram');
		tgToken = '';
		tgChat = '';
		tgTokenBad = false;
		tgChatBad = false;
		tgEditing = false;
	}

	function tgOpen(): void {
		tgEditing = true;
		tgChat = s.telegram.chat_id;
		tgFeedback = null;
		queueMicrotask(() => tgInput?.focus());
	}

	function tgKey(e: KeyboardEvent): void {
		if (e.key === 'Enter' && e.isTrusted) void tgCommitNow();
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

	/** Every event, each with a dot in the colour of what it reports. */
	const EVENTS = [
		{ key: 'claim', title: 'settings.n_claim', sub: 'settings.n_claim_sub', tone: 'ok' },
		{ key: 'auth', title: 'settings.n_auth', sub: 'settings.n_auth_sub', tone: 'warn' },
		{ key: 'errors', title: 'settings.n_errors', sub: 'settings.n_errors_sub', tone: 'bad' },
		{ key: 'daily_summary', title: 'settings.n_daily', sub: 'settings.n_daily_sub', tone: 'info' },
		{ key: 'reward_codes', title: 'settings.n_codes', sub: 'settings.n_codes_sub', tone: 'violet', since: '2.3' },
		{ key: 'progress', title: 'settings.n_progress', sub: 'settings.n_progress_sub', tone: 'info' },
		{ key: 'canary', title: 'settings.n_canary', sub: 'settings.n_canary_sub', tone: 'bad' }
	] as const;

	let avatar = $state('');
	let avatarDirty = $state(false);
	let avatarBad = $state(false);
	$effect(() => {
		if (!avatarDirty) avatar = s.notify_avatar_url;
	});

	async function saveAvatar(): Promise<boolean> {
		const v = avatar.trim();
		if (!isAvatarURL(v)) {
			avatarBad = true;
			return false;
		}
		const ok = await save('notify_avatar_url', { notify_avatar_url: v });
		if (ok) avatarDirty = false;
		return ok;
	}
	const avatarValid = (): boolean => isAvatarURL(avatar.trim());
	const saveAvatarNow = (): Promise<boolean> => auto.now('notify_avatar_url', saveAvatar, avatarValid);

	// User IDs pinged on error notifications, one per service.
	type MentionKey = 'discord_mention_id' | 'telegram_mention_id';
	let mention = $state<Record<MentionKey, string>>({ discord_mention_id: '', telegram_mention_id: '' });
	let mentionDirty = $state<Record<MentionKey, boolean>>({ discord_mention_id: false, telegram_mention_id: false });
	let mentionBad = $state<Record<MentionKey, boolean>>({ discord_mention_id: false, telegram_mention_id: false });
	$effect(() => {
		if (!mentionDirty.discord_mention_id) mention.discord_mention_id = s.discord_mention_id ?? '';
		if (!mentionDirty.telegram_mention_id) mention.telegram_mention_id = s.telegram_mention_id ?? '';
	});

	async function saveMention(key: MentionKey): Promise<boolean> {
		const v = mention[key].trim();
		if (!isMentionID(v)) {
			mentionBad[key] = true;
			return false;
		}
		const ok = await save(key, { [key]: v });
		if (ok) mentionDirty[key] = false;
		return ok;
	}
	const mentionRun = (key: MentionKey) => (): Promise<boolean> => saveMention(key);
	const mentionValid = (key: MentionKey) => (): boolean => isMentionID(mention[key].trim());

	function editMention(key: MentionKey): void {
		mentionDirty[key] = true;
		mentionBad[key] = false;
		auto.edit(key, mentionRun(key), mentionValid(key));
	}

	const MENTIONS = [
		{ key: 'discord_mention_id', label: 'settings.mention_discord', help: 'settings.mention_discord_help', ph: '123456789012345678' },
		{ key: 'telegram_mention_id', label: 'settings.mention_telegram', help: 'settings.mention_telegram_help', ph: '123456789' }
	] as const;
</script>

<section class="set hue-{SECTION_LOOK.notifications.hue}" id="notifications" aria-labelledby="h-not">
	<SectionHead sec="notifications" id="h-not">
		{#snippet chips()}
			{#if s.discord.configured}<span class="chip ok">{t('settings.chip_on', { name: t('settings.discord') })}</span>{/if}
			{#if s.telegram.configured}<span class="chip ok">{t('settings.chip_on', { name: t('settings.telegram') })}</span>{/if}
		{/snippet}
	</SectionHead>
	<div class="panel">
		<div class="card-body">
			<div class="title">
				<span class="brand discord" aria-hidden="true"><Icon name="chat" size={14} /></span>
				<b>{t('settings.discord')}</b>
				{#if s.discord.configured}
					<span class="chip ok">{t('settings.connected')}</span>
				{:else}
					<span class="chip off">{t('settings.not_connected')}</span>
				{/if}
			</div>
			<div class="field">
				<label for="discord-webhook">{t('settings.webhook_url')}</label>
				<div class="inline wrap">
					{#if showInput}
						<input
							class="inp masked grow"
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
							oninput={editHook}
							onkeydown={(e) => {
								if (e.key === 'Enter' && e.isTrusted) void commitNow();
								else if (e.key === 'Escape') cancel();
							}}
						/>
						<button type="button" class="btn" disabled={url.trim() === ''} onclick={commitNow}>{t('accounts.save')}</button>
						{#if s.discord.configured}
							<button type="button" class="btn ghost" onclick={cancel}>{t('accounts.cancel')}</button>
						{/if}
					{:else}
						<input class="inp grow" id="discord-webhook" type="text" value={s.discord.webhook_hint} readonly />
						<button type="button" class="btn" onclick={change}>{t('settings.change')}</button>
					{/if}
				</div>
				{#if invalid}<span class="feedback bad" role="alert">{t('settings.webhook_invalid')}</span>{/if}
				{#if errs.webhook}<span class="feedback bad">{t('settings.save_failed')}</span>{/if}
			</div>
		</div>
		<div class="card-body">
			<div class="title">
				<span class="brand telegram" aria-hidden="true"><Icon name="send" size={13} /></span>
				<b>{t('settings.telegram')}</b>
				{#if s.telegram.configured}
					<span class="chip ok">{t('settings.connected')}</span>
				{:else}
					<span class="chip off">{t('settings.not_connected')}</span>
				{/if}
				{#if !tgEditing && !s.telegram.configured}
					<button type="button" class="btn tint push" onclick={tgOpen}>{t('settings.connect')}</button>
				{/if}
			</div>
			{#if tgEditing}
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
							oninput={tgEdit}
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
							oninput={tgEdit}
							onkeydown={tgKey}
						/>
					</div>
				</div>
				<p class="help">{t('settings.telegram_help')}</p>
				<div class="inline wrap">
					<button type="button" class="btn" disabled={!tgCanSave} onclick={tgCommitNow}>{t('accounts.save')}</button>
					<button type="button" class="btn ghost" onclick={tgCancel}>{t('accounts.cancel')}</button>
				</div>
			{:else if s.telegram.configured}
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
					<button type="button" class="btn" onclick={tgOpen}>{t('settings.change')}</button>
				</div>
			{/if}
			{#if tgTokenBad}<span class="feedback bad" role="alert">{t('settings.token_invalid')}</span>{/if}
			{#if tgChatBad}<span class="feedback bad" role="alert">{t('settings.chat_invalid')}</span>{/if}
			{#if errs.telegram}<span class="feedback bad">{t('settings.save_failed')}</span>{/if}
		</div>
	</div>

	<div class="panel test">
		<div class="row">
			<div class="lbl">
				<b>{t('settings.test_notify')}</b>
				<small>{s.discord.configured || s.telegram.configured ? t('settings.test_notify_sub') : t('settings.test_notify_none')}</small>
				{#if feedback}<small class:bad={!feedback.ok} role="status">{feedback.text}</small>{/if}
				{#if tgFeedback}<small class:bad={!tgFeedback.ok} role="status">{tgFeedback.text}</small>{/if}
			</div>
			{#if s.discord.configured || s.telegram.configured}
				<div class="inline wrap">
					{#if s.discord.configured}
						<button type="button" class="btn" disabled={testing} onclick={sendTest}>{t('settings.test_discord')}</button>
					{/if}
					{#if s.telegram.configured}
						<button type="button" class="btn" disabled={tgTesting} onclick={tgSendTest}>{t('settings.test_telegram')}</button>
					{/if}
				</div>
			{:else}
				<span class="chip off">{t('settings.unavailable')}</span>
			{/if}
		</div>
	</div>

	<h3 class="sub-h"><span class="tile sm" aria-hidden="true"><Icon name="bell" size={13} /></span>{t('settings.notify_when')}</h3>
	<div class="panel">
		{#each EVENTS as k (k.key)}
			<div class="row ev">
				<span class="dot {k.tone}" aria-hidden="true"></span>
				<div class="lbl">
					<b id="n-{k.key}">{t(k.title)}{#if 'since' in k}<NewPill since={k.since} />{/if}</b><small>{t(k.sub)}</small>
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

	<details class="adv">
		<summary>{t('settings.advanced')}</summary>
		<div class="panel">
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
							oninput={() => {
								avatarDirty = true;
								avatarBad = false;
								auto.edit('notify_avatar_url', saveAvatar, avatarValid);
							}}
							onkeydown={(e) => {
								if (e.key === 'Enter' && e.isTrusted && avatarDirty) void saveAvatarNow();
							}}
						/>
						<button type="button" class="btn" disabled={!avatarDirty} onclick={saveAvatarNow}>{t('accounts.save')}</button>
					</div>
					<p class="help">{t('settings.avatar_help')}</p>
					{#if avatarBad}<span class="feedback bad" role="alert">{t('settings.avatar_invalid')}</span>{/if}
					{#if errs.notify_avatar_url}<span class="feedback bad">{t('settings.save_failed')}</span>{/if}
				</div>
			</div>
			<div class="card-body">
				<div class="title"><b>{t('settings.mention_title')}<NewPill since="2.3" /></b></div>
				{#each MENTIONS as m (m.key)}
					<div class="field">
						<label for={m.key}>{t(m.label)}</label>
						<div class="inline wrap">
							<input
								class="inp grow"
								id={m.key}
								name={m.key}
								type="text"
								inputmode="numeric"
								autocomplete="off"
								spellcheck="false"
								placeholder={m.ph}
								aria-invalid={mentionBad[m.key] ? true : undefined}
								bind:value={mention[m.key]}
								oninput={() => editMention(m.key)}
								onkeydown={(e) => {
									if (e.key === 'Enter' && e.isTrusted && mentionDirty[m.key]) void auto.now(m.key, mentionRun(m.key), mentionValid(m.key));
								}}
							/>
							<button
								type="button"
								class="btn"
								disabled={!mentionDirty[m.key]}
								onclick={() => auto.now(m.key, mentionRun(m.key), mentionValid(m.key))}>{t('accounts.save')}</button
							>
						</div>
						<p class="help">{t(m.help)}</p>
						{#if mentionBad[m.key]}<span class="feedback bad" role="alert">{t('settings.mention_invalid')}</span>{/if}
						{#if errs[m.key]}<span class="feedback bad">{t('settings.save_failed')}</span>{/if}
					</div>
				{/each}
			</div>
		</div>
	</details>
</section>

<style>
	.title {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
	}
	.title b {
		font-size: 13.5px;
		font-weight: 500;
	}
	/* Service marks: each service's own colour, white glyph. */
	.brand {
		width: 24px;
		height: 24px;
		border-radius: 7px;
		display: grid;
		place-items: center;
		flex: none;
		color: #fff;
	}
	.discord {
		background: #5865f2;
	}
	.telegram {
		background: #229ed9;
	}
	.push {
		margin-left: auto;
	}
	/* Beats the page's .btn rule. */
	.inline .btn.ghost {
		background: none;
		border-color: transparent;
		color: var(--t2);
	}
	.inline .btn.ghost:hover {
		color: var(--text);
	}
	/* Masked by CSS, not a password input, so password managers leave it alone. */
	.masked {
		-webkit-text-security: disc;
	}
	.field .inp[readonly] {
		color: var(--t2);
		background: var(--surface);
	}
	.bad {
		color: var(--red);
	}
	.help {
		margin: 0;
		color: var(--t2);
		font-size: 12.5px;
	}
	.test {
		margin-top: 14px;
	}
	.grow {
		flex: 1;
		min-width: 0;
	}
	.dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		flex: none;
		background: var(--t3);
	}
	.dot.ok {
		background: var(--green);
	}
	.dot.warn {
		background: var(--amber);
	}
	.dot.bad {
		background: var(--red);
	}
	.dot.info {
		background: var(--blue);
	}
	.dot.violet {
		background: var(--accent);
	}
	@media (max-width: 720px) {
		/* Dot, label and switch stay on one line. */
		.panel .row.ev {
			flex-wrap: nowrap;
		}
	}
</style>
