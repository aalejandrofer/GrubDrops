<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api } from '$lib/app';
	import { ApiError } from '$lib/api/client';
	import type { Account, DeviceStart, Platform } from '$lib/api/types';
	import {
		accountLine,
		accountStatus,
		androidErrorKey,
		createDeviceFlow,
		hasAndroidToken,
		initials,
		rowActions,
		type DeviceFlow,
		type DeviceState,
		type TwitchMethod
	} from '$lib/accounts';
	import CopyCommand from '$lib/components/CopyCommand.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { t } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';
	import { loginFlow } from '$lib/login-flow.svelte';

	const STATUS_KEYS = {
		mining: 'accounts.status.mining',
		signed_out: 'accounts.status.signed_out',
		idle: 'accounts.status.idle',
		paused: 'accounts.status.paused'
	} as const;

	const live = getLive();

	interface Panel {
		kind: Platform;
		id: string;
		isNew: boolean;
	}

	let panel = $state<Panel | null>(null);
	let busy = $state(false);
	let addError = $state('');
	let menu = $state<string | null>(null);
	let confirming = $state<string | null>(null);
	let rowError = $state<Record<string, string>>({});
	let renaming = $state<string | null>(null);
	let renameValue = $state('');

	let ds = $state<DeviceState>('idle');
	let dcode = $state<DeviceStart | null>(null);
	let flow: DeviceFlow | null = null;
	let cancelTimer: (() => void) | null = null;

	let method = $state<TwitchMethod | null>(null);
	let androidUrl = $state('');
	let paste = $state('');
	let androidMsg = $state('');
	let androidDone = $state(false);

	let cookies = $state('');
	let saving = $state(false);
	let kickMsg = $state('');
	let kickSaved = $state<'ok' | 'unverified' | null>(null);

	const runnerOf = (id: string) => live.runners.find((r) => r.account_id === id);
	const shown = (a: Account) => !(panel?.isNew && panel.id === a.id);

	function schedule(fn: () => void, ms: number): () => void {
		const id = setTimeout(fn, ms);
		cancelTimer = () => clearTimeout(id);
		return () => clearTimeout(id);
	}

	function errMsg(e: unknown, fallback: string): string {
		return e instanceof ApiError && e.body.message ? e.body.message : fallback;
	}

	function reset(): void {
		cancelTimer?.();
		cancelTimer = null;
		flow = null;
		panel = null;
		loginFlow.id = null;
		ds = 'idle';
		dcode = null;
		method = null;
		androidUrl = '';
		paste = '';
		androidMsg = '';
		androidDone = false;
		cookies = '';
		kickMsg = '';
		kickSaved = null;
		saving = false;
	}

	async function openFlow(kind: Platform, existing?: string): Promise<void> {
		if (busy) return;
		menu = null;
		addError = '';
		if (panel) await closePanel();
		if (kind === 'twitch') {
			// Twitch asks for the method first; the TV path creates the account on pick.
			panel = { kind, id: existing ?? '', isNew: existing === undefined };
			loginFlow.id = existing ?? null;
			return;
		}
		busy = true;
		try {
			let id = existing;
			if (!id) {
				const created = await api.addAccount(kind);
				id = created.id;
			}
			panel = { kind, id, isNew: existing === undefined };
			loginFlow.id = id;
		} catch (e) {
			addError = errMsg(e, t('accounts.add_failed'));
		} finally {
			busy = false;
		}
	}

	async function chooseTV(): Promise<void> {
		const p = panel;
		if (!p || busy) return;
		busy = true;
		try {
			if (!p.id) {
				const created = await api.addAccount('twitch');
				p.id = created.id;
				loginFlow.id = created.id;
			}
			method = 'tv';
			const f = createDeviceFlow({ api, accountId: p.id, schedule, isNew: p.isNew });
			f.onChange(() => {
				ds = f.state;
				dcode = f.code;
			});
			flow = f;
			await f.start();
		} catch (e) {
			addError = errMsg(e, t('accounts.add_failed'));
			reset();
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
		const p = panel;
		if (!p || saving) return;
		androidMsg = '';
		if (!hasAndroidToken(paste)) {
			androidMsg = t('accounts.android_no_token');
			return;
		}
		saving = true;
		try {
			await api.twitchAndroidLogin(paste.trim(), p.isNew ? undefined : p.id);
			paste = '';
			androidDone = true;
		} catch (e) {
			androidMsg = t(androidErrorKey(e instanceof ApiError ? e.code : undefined));
		} finally {
			saving = false;
		}
	}

	async function closePanel(): Promise<void> {
		const p = panel;
		const f = flow;
		try {
			if (p?.kind === 'twitch' && f) await f.cancel();
			else if (p?.kind === 'kick' && p.isNew && !kickSaved) await api.removeAccount(p.id);
		} catch {
			addError = t('accounts.remove_failed');
		}
		reset();
	}

	async function finish(): Promise<void> {
		reset();
		await live.refetchAll().catch(() => {});
	}

	async function saveKick(): Promise<void> {
		if (!panel || saving) return;
		saving = true;
		kickMsg = '';
		try {
			const r = await api.kickSession(panel.id, cookies);
			cookies = '';
			if (r.verified) {
				kickSaved = 'ok';
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

	function startRename(a: Account): void {
		renaming = a.id;
		renameValue = a.name;
		rowError[a.id] = '';
	}

	async function commitRename(a: Account): Promise<void> {
		const name = renameValue.trim();
		renaming = null;
		if (name === '' || name === a.name) return;
		try {
			await api.renameAccount(a.id, name);
		} catch (e) {
			rowError[a.id] = errMsg(e, t('accounts.rename_failed'));
		}
	}

	function focusSelect(node: HTMLInputElement): void {
		node.focus();
		node.select();
	}

	async function remove(a: Account): Promise<void> {
		rowError[a.id] = '';
		try {
			await api.removeAccount(a.id);
			confirming = null;
		} catch (e) {
			rowError[a.id] = errMsg(e, t('accounts.remove_failed'));
		}
	}

	async function setEnabled(a: Account, enabled: boolean): Promise<void> {
		menu = null;
		rowError[a.id] = '';
		try {
			await api.setAccountEnabled(a.id, enabled);
			await live.refetchAll().catch(() => {});
		} catch (e) {
			rowError[a.id] = errMsg(e, t('accounts.pause_failed', { name: a.name }));
		}
	}

	function openMenu(id: string): void {
		menu = menu === id ? null : id;
		confirming = null;
	}

	function onWindowClick(e: MouseEvent): void {
		// composedPath still lists a clicked button the click handler has already replaced.
		const inside = e.composedPath().some((n) => n instanceof Element && n.classList.contains('menu-wrap'));
		if (menu && !inside) {
			menu = null;
			confirming = null;
		}
	}

	onMount(() => {
		const add = new URL(location.href).searchParams.get('add');
		if (add === 'twitch' || add === 'kick') {
			void goto(location.pathname + location.hash, { replace: true });
			void openFlow(add);
		}
	});

	onDestroy(() => {
		flow?.dispose();
		cancelTimer?.();
		loginFlow.id = null;
		// Leaving mid-flow drops the half-made account.
		const p = panel;
		if (p?.isNew && p.id && !kickSaved && ds !== 'done') api.removeAccount(p.id).catch(() => {});
	});
</script>

<svelte:window
	onclick={onWindowClick}
	onkeydown={(e) => {
		if (e.key === 'Escape') {
			menu = null;
			confirming = null;
		}
	}}
/>

{#snippet flowPanel()}
	{#if panel}
		<div class="card-body flow" aria-live="polite">
			<div class="card-title">
				<PlatformMark platform={panel.kind} />
				<b>{t(panel.kind === 'twitch' ? 'accounts.twitch' : 'accounts.kick')}</b>
			</div>
			{#if panel.kind === 'twitch' && method === null}
				<div class="inline wrap">
					<button type="button" class="btn primary" disabled={busy} onclick={chooseAndroid}>{t('accounts.method_android')}</button>
					<button type="button" class="btn" disabled={busy} onclick={chooseTV}>{t('accounts.method_tv')}</button>
					<button type="button" class="btn" onclick={closePanel}>{t('accounts.cancel')}</button>
				</div>
			{:else if panel.kind === 'twitch' && method === 'android'}
				{#if androidDone}
					<p class="feedback">{t('accounts.device_done')}</p>
					<div class="inline"><button type="button" class="btn" onclick={finish}>{t('accounts.done')}</button></div>
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
						<label for="twitch-paste">{t('accounts.android_paste_label')}</label>
						<input
							class="inp"
							id="twitch-paste"
							type="text"
							spellcheck="false"
							autocomplete="off"
							bind:value={paste}
							onkeydown={(e) => {
								if (e.key === 'Enter') void submitAndroid();
							}}
						/>
					</div>
					<div class="inline wrap">
						<button type="button" class="btn primary" disabled={saving || paste.trim() === ''} onclick={submitAndroid}>
							{saving ? t('accounts.saving') : t('accounts.android_submit')}
						</button>
						<button type="button" class="btn" onclick={closePanel}>{t('accounts.cancel')}</button>
					</div>
					{#if androidMsg}<small class="bad" role="alert">{androidMsg}</small>{/if}
				{/if}
			{:else if panel.kind === 'twitch'}
				{#if ds === 'code' && dcode}
					<p class="go">{t('accounts.device_go', { url: dcode.verification_url })}</p>
					<div class="code">{dcode.user_code}</div>
					<CopyCommand command={dcode.user_code} />
					<small class="wait">{t('accounts.device_waiting')}</small>
				{:else if ds === 'done'}
					<p class="feedback">{t('accounts.device_done')}</p>
				{:else if ds === 'expired'}
					<p class="bad">{t('accounts.device_expired')}</p>
				{:else if ds === 'error'}
					<p class="bad">{t('accounts.device_error')}</p>
				{/if}
				<div class="inline wrap">
					{#if ds === 'done'}
						<button type="button" class="btn" onclick={finish}>{t('accounts.done')}</button>
					{:else}
						{#if ds === 'expired' || ds === 'error'}
							<button type="button" class="btn primary" onclick={() => flow?.start()}>{t('accounts.new_code')}</button>
						{/if}
						<button type="button" class="btn" onclick={closePanel}>{t('accounts.cancel')}</button>
					{/if}
				</div>
			{:else if kickSaved === 'ok'}
				<p class="feedback">{t('accounts.device_done')}</p>
				<div class="inline"><button type="button" class="btn" onclick={finish}>{t('accounts.done')}</button></div>
			{:else}
				<div class="field">
					<label for="kick-cookies">{t('accounts.cookies_label')}</label>
					<textarea
						class="inp ta"
						id="kick-cookies"
						rows="5"
						spellcheck="false"
						autocomplete="off"
						bind:value={cookies}
					></textarea>
					<small class="help">{t('accounts.cookies_help')}</small>
				</div>
				<div class="inline wrap">
					<label class="btn file">
						{t('accounts.cookies_file')}
						<input type="file" accept=".txt,text/plain" onchange={onFile} />
					</label>
					<button type="button" class="btn primary" disabled={saving || cookies.trim() === ''} onclick={saveKick}>
						{saving ? t('accounts.saving') : t('accounts.save')}
					</button>
					<button type="button" class="btn" onclick={closePanel}>{kickSaved ? t('accounts.close') : t('accounts.cancel')}</button>
				</div>
				{#if kickMsg}<small class="bad" role="alert">{kickMsg}</small>{/if}
			{/if}
		</div>
	{/if}
{/snippet}

<section class="set" id="accounts" aria-labelledby="h-acc">
	<div class="sec-head">
		<div>
			<h2 id="h-acc">{t('settings.accounts')}</h2>
			<p>{t('accounts.intro')}</p>
		</div>
		<div class="menu-wrap">
			<button type="button" class="btn" aria-haspopup="menu" aria-expanded={menu === 'add'} disabled={busy} onclick={() => openMenu('add')}>
				<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>{t('accounts.add')}
			</button>
			{#if menu === 'add'}
				<div class="menu" role="menu">
					<div class="note">{t('accounts.sign_in_with')}</div>
					<button type="button" role="menuitem" onclick={() => openFlow('twitch')}><PlatformMark platform="twitch" />{t('accounts.twitch')}</button>
					<button type="button" role="menuitem" onclick={() => openFlow('kick')}><PlatformMark platform="kick" />{t('accounts.kick')}</button>
				</div>
			{/if}
		</div>
	</div>
	{#if addError}<p class="bad top" role="alert">{addError}</p>{/if}
	<div class="panel" id="acct-list" class:none={!panel?.isNew && live.accounts.filter(shown).length === 0}>
		{#if panel?.isNew}{@render flowPanel()}{/if}
		{#each live.accounts.filter(shown) as a (a.id)}
			{@const r = runnerOf(a.id)}
			{@const st = accountStatus(a, r)}
			{@const acts = rowActions(a, r)}
			<div class="row acct" class:problem={st === 'signed_out'}>
				<span class="av">
					{#if a.avatar_url}<img src={a.avatar_url} alt={t('accounts.avatar', { name: a.name })} />{:else}{initials(a.name)}{/if}
					<span class="mk"><PlatformMark platform={a.platform} /></span>
				</span>
				<div class="who">
					{#if renaming === a.id}
						<input
							class="inp rename"
							aria-label={t('accounts.rename_label', { name: a.name })}
							bind:value={renameValue}
							use:focusSelect
							onkeydown={(e) => {
								if (e.key === 'Enter') void commitRename(a);
								else if (e.key === 'Escape') renaming = null;
							}}
							onblur={() => {
								if (renaming === a.id) void commitRename(a);
							}}
						/>
					{:else}
						<b><button type="button" class="nm" title={t('accounts.rename_label', { name: a.name })} onclick={() => startRename(a)}>{a.name}</button></b>
					{/if}
					<small>{accountLine(a, r)}</small>
					{#if rowError[a.id]}<small class="bad" role="alert">{rowError[a.id]}</small>{/if}
				</div>
				<span class="st {st}">{t(STATUS_KEYS[st])}</span>
				{#if acts.includes('login')}
					<button type="button" class="btn primary fix" disabled={busy} onclick={() => openFlow(a.platform, a.id)}>
						<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4M10 17l5-5-5-5M15 12H3" /></svg>{t('accounts.log_in_again')}
					</button>
				{/if}
				{#if acts.includes('resume')}
					<button type="button" class="btn fix" onclick={() => setEnabled(a, true)}>{t('accounts.resume')}</button>
				{/if}
				<div class="menu-wrap">
					<button
						type="button"
						class="ico"
						aria-label={t('accounts.more_for', { name: a.name })}
						aria-haspopup="menu"
						aria-expanded={menu === a.id}
						onclick={() => openMenu(a.id)}
					>
						<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12h.01M12 12h.01M19 12h.01" stroke-width="3" /></svg>
					</button>
					{#if menu === a.id}
						<div class="menu" role="menu">
							{#if confirming === a.id}
								<div class="note">{t('accounts.remove_confirm', { name: a.name })}</div>
								<div class="conf">
									<button type="button" class="danger" onclick={() => remove(a)}>{t('accounts.remove_yes')}</button>
									<button type="button" onclick={() => (confirming = null)}>{t('accounts.cancel')}</button>
								</div>
							{:else}
								<button type="button" role="menuitem" onclick={() => setEnabled(a, !a.enabled)}>
									{t(a.enabled ? 'accounts.pause' : 'accounts.resume')}
								</button>
								<button type="button" role="menuitem" class="danger" onclick={() => (confirming = a.id)}>
									<Icon name="x" />{t('accounts.remove')}
								</button>
							{/if}
						</div>
					{/if}
				</div>
			</div>
			{#if panel && !panel.isNew && panel.id === a.id}{@render flowPanel()}{/if}
		{/each}
	</div>
</section>

<style>
	.panel.none {
		display: none;
	}
	.top {
		margin: 0 0 10px;
		font-size: 12.5px;
	}
	.bad {
		color: var(--red);
		font-size: 12.5px;
		margin: 0;
	}
	small.bad {
		display: block;
	}
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
	}
	.menu button:hover {
		background: var(--surface-3);
	}
	.menu button:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.menu .danger {
		color: var(--red);
	}
	.menu .note {
		padding: 4px 8px 6px;
		color: var(--t2);
		font-size: 12px;
	}
	.conf {
		display: flex;
		gap: 4px;
	}
	.conf button {
		justify-content: center;
	}
	.i {
		width: 14px;
		height: 14px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.8;
		stroke-linecap: round;
		stroke-linejoin: round;
		flex: none;
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
	.ico:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	section .btn.primary {
		background: var(--accent-strong);
		border-color: var(--accent-strong);
		color: #fff;
	}
	section .btn.primary:hover:not(:disabled) {
		background: color-mix(in srgb, var(--accent-strong) 85%, #fff);
		border-color: color-mix(in srgb, var(--accent-strong) 85%, #fff);
	}
	.acct .av {
		width: 32px;
		height: 32px;
		border-radius: 50%;
		display: grid;
		place-items: center;
		flex: none;
		font-size: 12px;
		font-weight: 600;
		background: var(--surface-3);
		color: var(--text);
		position: relative;
	}
	.acct .av img {
		width: 100%;
		height: 100%;
		border-radius: 50%;
		object-fit: cover;
	}
	.acct .av .mk {
		position: absolute;
		right: -3px;
		bottom: -3px;
		display: grid;
		border-radius: 4px;
		box-shadow: 0 0 0 2px var(--surface);
	}
	.acct .who {
		flex: 1;
		min-width: 0;
	}
	.acct .who b {
		font-weight: 500;
		font-size: 13.5px;
		display: block;
	}
	.nm {
		border: 0;
		background: none;
		padding: 0;
		font: inherit;
		color: inherit;
		cursor: text;
		max-width: 100%;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		text-align: left;
	}
	.nm:hover {
		text-decoration: underline dotted;
		text-underline-offset: 3px;
	}
	.nm:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
		border-radius: 3px;
	}
	.rename {
		height: 28px;
		font-family: var(--font);
		font-size: 13.5px;
	}
	.acct .who small {
		display: block;
		color: var(--t2);
		font-size: 12.5px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.acct .who small.bad {
		color: var(--red);
		white-space: normal;
	}
	.st {
		font-size: 12px;
		font-weight: 500;
		padding: 2px 8px;
		border-radius: 10px;
		display: inline-flex;
		align-items: center;
		gap: 6px;
		white-space: nowrap;
		color: var(--t2);
		background: var(--surface-3);
	}
	.st::before {
		content: '';
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: currentColor;
	}
	.st.mining {
		color: var(--green);
		background: var(--green-soft);
	}
	.st.signed_out {
		color: var(--red);
		background: var(--red-soft);
	}
	.st.paused {
		color: var(--amber);
		background: var(--amber-soft);
	}
	.acct.problem {
		background: linear-gradient(90deg, color-mix(in srgb, var(--red) 7%, transparent), transparent 60%);
		box-shadow: inset 2px 0 0 var(--red);
	}
	.acct.problem .who small:not(.bad) {
		color: color-mix(in srgb, var(--red) 55%, var(--text));
	}
	.flow + .row,
	.row + .flow {
		border-top: 1px solid var(--border-soft);
	}
	.go {
		margin: 0;
		font-size: 13px;
		overflow-wrap: anywhere;
	}
	.code {
		font: 600 28px/1.1 var(--mono);
		letter-spacing: 0.12em;
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
	.feedback {
		margin: 0;
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

	@media (max-width: 720px) {
		.acct .menu-wrap {
			order: 2;
		}
		.acct .st {
			order: 3;
		}
		.acct .fix {
			order: 4;
			width: 100%;
			justify-content: center;
		}
	}
</style>
