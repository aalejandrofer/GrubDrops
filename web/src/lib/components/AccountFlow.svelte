<script lang="ts">
	import { onDestroy, onMount, untrack } from 'svelte';
	import { api } from '$lib/app';
	import { ApiError } from '$lib/api/client';
	import type { DeviceStart, Platform } from '$lib/api/types';
	import { activateURL, androidErrorKey, createDeviceFlow, hasAndroidToken, toBase64, type DeviceFlow, type DeviceState, type TwitchMethod } from '$lib/accounts';
	import { t } from '$lib/i18n/index.svelte';
	import { loginFlow } from '$lib/login-flow.svelte';
	import CopyCommand from './CopyCommand.svelte';
	import PlatformMark from './PlatformMark.svelte';

	/**
	 * Sign-in flow for one Twitch or Kick account: Twitch Android paste, Twitch TV
	 * code, or Kick cookies.txt. A new Kick account is created on the first Save, a new TV
	 * account when the code is requested. Unmounting before sign-in drops the half-made account.
	 */
	let {
		kind,
		existing,
		start = null,
		framed = true,
		id = $bindable(''),
		busy = $bindable(false),
		onclose,
		ondone,
		onadded,
		onfail
	}: {
		kind: Platform;
		/** Account to sign in again; omitted for a new account. */
		existing?: string;
		/** Twitch method to open right away; null shows the method buttons. */
		start?: TwitchMethod | null;
		/** Title row and Cancel button. */
		framed?: boolean;
		/** The account id once one exists. */
		id?: string;
		/** Creating the account. */
		busy?: boolean;
		/** After Cancel cleaned up. */
		onclose?: () => void;
		/** Done button after sign-in; no button when omitted. */
		ondone?: () => void;
		/** Sign-in finished. */
		onadded?: () => void;
		/** The account could not be created; the flow is unusable. */
		onfail?: (msg: string) => void;
	} = $props();

	// The props are fixed for one mount; the parent remounts for a new flow.
	const first = untrack(() => existing);
	const isNew = first === undefined;
	id = first ?? '';

	let ds = $state<DeviceState>('idle');
	let dcode = $state<DeviceStart | null>(null);
	const activateHref = $derived(dcode ? activateURL(dcode.verification_url, dcode.user_code) : '');
	let flow: DeviceFlow | null = null;
	let cancelTimer: (() => void) | null = null;

	let method = $state<TwitchMethod | null>(null);
	// Shape of the redirect URL, token hidden.
	const ANDROID_EXAMPLE = 'https://www.twitch.tv/#access_token=XXX&scope=&token_type=bearer';
	let androidUrl = $state('');
	let paste = $state('');
	let androidMsg = $state('');
	let androidDone = $state(false);

	let cookies = $state('');
	let saving = $state(false);
	let kickMsg = $state('');
	let kickSaved = $state<'ok' | 'unverified' | null>(null);
	let closed = false;

	const uid = $props.id();

	function schedule(fn: () => void, ms: number): () => void {
		const h = setTimeout(fn, ms);
		cancelTimer = () => clearTimeout(h);
		return () => clearTimeout(h);
	}

	function errMsg(e: unknown, fallback: string): string {
		return e instanceof ApiError && e.body.message ? e.body.message : fallback;
	}

	function fail(e: unknown): void {
		closed = true;
		onfail?.(errMsg(e, t('accounts.add_failed')));
	}

	/** Creates the new Kick account. Returns false when the flow closed or failed. */
	async function createKick(): Promise<boolean> {
		busy = true;
		try {
			const created = await api.addAccount('kick');
			if (closed) {
				await api.removeAccount(created.id).catch(() => {});
				return false;
			}
			id = created.id;
			loginFlow.id = created.id;
			return true;
		} catch (e) {
			fail(e);
			return false;
		} finally {
			busy = false;
		}
	}

	async function chooseTV(): Promise<void> {
		if (busy) return;
		busy = true;
		try {
			if (!id) {
				const created = await api.addAccount('twitch');
				if (closed) {
					await api.removeAccount(created.id).catch(() => {});
					return;
				}
				id = created.id;
				loginFlow.id = created.id;
			}
			method = 'tv';
			const f = createDeviceFlow({ api, accountId: id, schedule, isNew });
			f.onChange(() => {
				ds = f.state;
				dcode = f.code;
				if (f.state === 'done') onadded?.();
			});
			flow = f;
			await f.start();
		} catch (e) {
			fail(e);
		} finally {
			busy = false;
		}
	}

	async function chooseAndroid(): Promise<void> {
		method = 'android';
		androidMsg = '';
		try {
			androidUrl = (await api.twitchAndroidUrl()).login_url;
		} catch {
			androidMsg = t('accounts.android_failed');
		}
	}

	async function submitAndroid(): Promise<void> {
		if (saving) return;
		androidMsg = '';
		if (!hasAndroidToken(paste)) {
			androidMsg = t('accounts.android_no_token');
			return;
		}
		saving = true;
		try {
			await api.twitchAndroidLogin(paste.trim(), isNew ? undefined : id);
			paste = '';
			androidDone = true;
			onadded?.();
		} catch (e) {
			androidMsg = t(androidErrorKey(e instanceof ApiError ? e.code : undefined));
		} finally {
			saving = false;
		}
	}

	async function onJar(e: Event & { currentTarget: HTMLInputElement }): Promise<void> {
		const input = e.currentTarget;
		const file = input.files?.[0];
		input.value = '';
		if (!file || saving) return;
		androidMsg = '';
		let jar: string;
		try {
			jar = toBase64(new Uint8Array(await file.arrayBuffer()));
		} catch {
			androidMsg = t('accounts.file_failed');
			return;
		}
		saving = true;
		try {
			await api.twitchTdmLogin(jar, isNew ? undefined : id);
			androidDone = true;
			onadded?.();
		} catch (e) {
			androidMsg = t(androidErrorKey(e instanceof ApiError ? e.code : undefined));
		} finally {
			saving = false;
		}
	}

	async function saveKick(): Promise<void> {
		if (saving || busy) return;
		if (!id && !(await createKick())) return;
		saving = true;
		kickMsg = '';
		try {
			const r = await api.kickSession(id, cookies);
			cookies = '';
			if (r.verified) {
				kickSaved = 'ok';
				onadded?.();
			} else {
				kickSaved = 'unverified';
				kickMsg = t('accounts.kick_unverified');
			}
		} catch (e) {
			kickMsg = errMsg(e, t('accounts.kick_failed'));
		} finally {
			saving = false;
		}
	}

	async function onFile(e: Event & { currentTarget: HTMLInputElement }): Promise<void> {
		const input = e.currentTarget;
		const file = input.files?.[0];
		if (!file) return;
		kickMsg = '';
		try {
			cookies = await file.text();
		} catch {
			kickMsg = t('accounts.file_failed');
		}
		input.value = '';
	}

	/** Stops the flow and drops a half-made account. Throws when the removal fails. */
	export async function cancel(): Promise<void> {
		if (closed) return;
		closed = true;
		cancelTimer?.();
		const f = flow;
		flow = null;
		loginFlow.id = null;
		if (kind === 'twitch' && f) await f.cancel();
		else if (kind === 'kick' && isNew && id && !kickSaved) await api.removeAccount(id);
	}

	async function close(): Promise<void> {
		try {
			await cancel();
		} finally {
			onclose?.();
		}
	}

	onMount(() => {
		loginFlow.id = first ?? null;
		if (kind === 'twitch' && start === 'tv') void chooseTV();
		else if (kind === 'twitch' && start === 'android') void chooseAndroid();
		else if (kind === 'twitch' && start === 'tdm') method = 'tdm';
	});

	onDestroy(() => {
		flow?.dispose();
		cancelTimer?.();
		loginFlow.id = null;
		if (closed) return;
		closed = true;
		// Leaving mid-flow drops the half-made account. A save in flight may still succeed, so it stays.
		if (isNew && id && !saving && !kickSaved && ds !== 'done') api.removeAccount(id).catch(() => {});
	});
</script>

<div class="card-body flow" aria-live="polite">
	{#if framed}
		<div class="card-title">
			<PlatformMark platform={kind} />
			<b>{t(kind === 'twitch' ? 'accounts.twitch' : 'accounts.kick')}</b>
		</div>
	{/if}
	{#if kind === 'twitch' && method === null}
		{#if start === null}
			<div class="inline wrap">
				<button type="button" class="btn primary" disabled={busy} onclick={chooseAndroid}>{t('accounts.method_android')}</button>
				<button type="button" class="btn" disabled={busy} onclick={chooseTV}>{t('accounts.method_tv')}</button>
				<button type="button" class="btn" disabled={busy} onclick={() => (method = 'tdm')}>{t('accounts.method_tdm')}</button>
				{#if framed}<button type="button" class="btn" onclick={close}>{t('accounts.cancel')}</button>{/if}
			</div>
		{/if}
	{:else if kind === 'twitch' && method === 'android'}
		{#if androidDone}
			<p class="feedback">{t('accounts.device_done')}</p>
			{#if ondone}<div class="inline"><button type="button" class="btn" onclick={ondone}>{t('accounts.done')}</button></div>{/if}
		{:else}
			<ol class="steps">
				<li>
					<span>{t('accounts.android_step1')}</span>
					{#if androidUrl}
						<a class="btn" href={androidUrl} target="_blank" rel="noopener noreferrer">{t('accounts.android_open')}</a>
					{/if}
				</li>
				<li><span class="go">{t('accounts.android_step2')}</span></li>
			</ol>
			<div class="field">
				<label for="{uid}-paste">{t('accounts.android_paste_label')}</label>
				<input
					class="inp"
					id="{uid}-paste"
					type="text"
					spellcheck="false"
					autocomplete="off"
					bind:value={paste}
					onkeydown={(e) => {
						if (e.key === 'Enter') void submitAndroid();
					}}
				/>
				<small class="eg">{t('accounts.android_example')} <code>{ANDROID_EXAMPLE}</code></small>
			</div>
			<div class="inline wrap">
				<button type="button" class="btn primary" disabled={saving || paste.trim() === ''} onclick={submitAndroid}>
					{saving ? t('accounts.saving') : t('accounts.android_submit')}
				</button>
				{#if framed}<button type="button" class="btn" onclick={close}>{t('accounts.cancel')}</button>{/if}
			</div>
			{#if androidMsg}<small class="bad" role="alert">{androidMsg}</small>{/if}
		{/if}
	{:else if kind === 'twitch' && method === 'tdm'}
		{#if androidDone}
			<p class="feedback">{t('accounts.device_done')}</p>
			{#if ondone}<div class="inline"><button type="button" class="btn" onclick={ondone}>{t('accounts.done')}</button></div>{/if}
		{:else}
			<div class="tdm">
				<label class="btn file">
					{saving ? t('accounts.saving') : t('accounts.tdm_import')}
					<input type="file" disabled={saving} onchange={onJar} />
				</label>
				<small class="help">{t('accounts.tdm_help')}</small>
			</div>
			{#if framed}<div class="inline"><button type="button" class="btn" onclick={close}>{t('accounts.cancel')}</button></div>{/if}
			{#if androidMsg}<small class="bad" role="alert">{androidMsg}</small>{/if}
		{/if}
	{:else if kind === 'twitch'}
		{#if ds === 'code' && dcode}
			<p class="go">{t('accounts.device_go')}</p>
			<a class="btn primary open" href={activateHref} target="_blank" rel="noopener noreferrer">
				{t('accounts.device_open')}
			</a>
			<CopyCommand command={activateHref} />
			<small class="wait">{t('accounts.device_waiting')}</small>
			<small class="warn" role="note"><span aria-hidden="true">!</span>{t('accounts.tv_no_discovery')}</small>
		{:else if ds === 'done'}
			<p class="feedback">{t('accounts.device_done')}</p>
		{:else if ds === 'expired'}
			<p class="bad">{t('accounts.device_expired')}</p>
		{:else if ds === 'error'}
			<p class="bad">{t('accounts.device_error')}</p>
		{/if}
		{#if ds === 'done'}
			{#if ondone}<div class="inline"><button type="button" class="btn" onclick={ondone}>{t('accounts.done')}</button></div>{/if}
		{:else if framed || ds === 'expired' || ds === 'error'}
			<div class="inline wrap">
				{#if ds === 'expired' || ds === 'error'}
					<button type="button" class="btn primary" onclick={() => flow?.start()}>{t('accounts.new_code')}</button>
				{/if}
				{#if framed}<button type="button" class="btn" onclick={close}>{t('accounts.cancel')}</button>{/if}
			</div>
		{/if}
	{:else if kickSaved === 'ok'}
		<p class="feedback">{t('accounts.device_done')}</p>
		{#if ondone}<div class="inline"><button type="button" class="btn" onclick={ondone}>{t('accounts.done')}</button></div>{/if}
	{:else}
		<div class="field">
			<label for="{uid}-cookies">{t('accounts.cookies_label')}</label>
			<textarea class="inp ta" id="{uid}-cookies" rows="5" spellcheck="false" autocomplete="off" bind:value={cookies}></textarea>
			<small class="help">{t('accounts.cookies_help')}</small>
		</div>
		<div class="inline wrap">
			<label class="btn file">
				{t('accounts.cookies_file')}
				<input type="file" accept=".txt,text/plain" onchange={onFile} />
			</label>
			<button type="button" class="btn primary" disabled={busy || saving || cookies.trim() === ''} onclick={saveKick}>
				{saving ? t('accounts.saving') : t('accounts.save')}
			</button>
			{#if framed}
				<button type="button" class="btn" onclick={close}>{kickSaved ? t('accounts.close') : t('accounts.cancel')}</button>
			{/if}
		</div>
		{#if kickMsg}<small class="bad" role="alert">{kickMsg}</small>{/if}
	{/if}
</div>

<style>
	.eg {
		display: block;
		margin-top: 6px;
		font-size: 12px;
		color: var(--t2);
		overflow-wrap: anywhere;
	}
	.eg code {
		font-family: var(--mono, ui-monospace, monospace);
		font-size: 12px;
	}
	/* Same values as the Settings primitives, so the flow looks alike in Settings and onboarding. */
	.card-body {
		padding: 14px 16px;
		display: grid;
		gap: 12px;
	}
	.card-title {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
	}
	.card-title b {
		font-size: 13.5px;
		font-weight: 500;
	}
	.inline {
		display: flex;
		gap: 8px;
		align-items: center;
		min-width: 0;
	}
	.btn {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font: inherit;
		font-size: 13px;
		font-weight: 500;
		padding: 5px 12px;
		border-radius: 7px;
		border: 1px solid var(--border);
		background: var(--surface-2);
		color: var(--text);
		white-space: nowrap;
		text-decoration: none;
		cursor: pointer;
	}
	.btn:hover:not(:disabled) {
		border-color: var(--t2);
	}
	.btn:disabled {
		opacity: 0.6;
		cursor: default;
	}
	.btn:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.btn.primary {
		background: var(--accent-strong);
		border-color: var(--accent-strong);
		color: #fff;
	}
	.btn.primary:hover:not(:disabled) {
		background: color-mix(in srgb, var(--accent-strong) 85%, #fff);
		border-color: color-mix(in srgb, var(--accent-strong) 85%, #fff);
	}
	.field {
		display: grid;
		gap: 6px;
	}
	.field label {
		font-size: 12.5px;
		font-weight: 500;
		color: var(--t2);
	}
	.inp {
		height: 34px;
		padding: 0 10px;
		border: 1px solid var(--border);
		border-radius: 7px;
		background: var(--bg);
		color: var(--text);
		font: 12.5px var(--mono);
		min-width: 0;
		width: 100%;
		caret-color: var(--accent);
	}
	.inp:focus {
		outline: 0;
		border-color: var(--accent);
		box-shadow: 0 0 0 3px var(--accent-soft);
	}
	.bad {
		color: var(--red);
		font-size: 12.5px;
		margin: 0;
	}
	small.bad {
		display: block;
	}
	.feedback {
		margin: 0;
		font-size: 12px;
		color: var(--green);
	}
	.open {
		justify-self: start;
	}
	.go {
		margin: 0;
		font-size: 13px;
		overflow-wrap: anywhere;
	}
	.wait {
		color: var(--t2);
		font-size: 12.5px;
	}
	.help {
		color: var(--t2);
		font-size: 12px;
	}
	.ta {
		height: auto;
		padding: 8px 10px;
		resize: vertical;
		white-space: pre;
		overflow: auto;
	}
	.file {
		position: relative;
		overflow: hidden;
	}
	.file input {
		position: absolute;
		inset: 0;
		opacity: 0;
		cursor: pointer;
		font-size: 0;
	}
	.file:focus-within {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.tdm {
		display: grid;
		gap: 6px;
		justify-items: start;
	}
	.warn {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		color: var(--amber);
		font-size: 12px;
	}
	.warn span {
		display: inline-grid;
		place-items: center;
		width: 15px;
		height: 15px;
		flex: none;
		border-radius: 50%;
		background: var(--amber-soft);
		font-weight: 700;
		font-size: 11px;
	}
	.steps {
		margin: 0;
		padding-left: 20px;
		display: grid;
		gap: 8px;
		font-size: 13px;
	}
	.steps li {
		overflow-wrap: anywhere;
	}
	.steps li .btn {
		margin-left: 8px;
	}
	@media (max-width: 420px) {
		.inline.wrap {
			flex-wrap: wrap;
		}
		.inline.wrap .btn {
			width: 100%;
			justify-content: center;
		}
	}
</style>
