<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api } from '$lib/app';
	import { ApiError } from '$lib/api/client';
	import type { Account, Platform } from '$lib/api/types';
	import { accountLine, accountStatus, initials, NAME_MAX, renameRequest, rowActions } from '$lib/accounts';
	import AccountFlow from '$lib/components/AccountFlow.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { t } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';

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

	let flowEl = $state<AccountFlow | null>(null);

	const runnerOf = (id: string) => live.runners.find((r) => r.account_id === id);
	const shown = (a: Account) => !(panel?.isNew && panel.id === a.id);

	function errMsg(e: unknown, fallback: string): string {
		return e instanceof ApiError && e.body.message ? e.body.message : fallback;
	}

	async function openFlow(kind: Platform, existing?: string): Promise<void> {
		if (busy) return;
		menu = null;
		addError = '';
		if (panel) await closePanel();
		panel = { kind, id: existing ?? '', isNew: existing === undefined };
	}

	async function closePanel(): Promise<void> {
		try {
			await flowEl?.cancel();
		} catch {
			addError = t('accounts.remove_failed');
		}
		panel = null;
	}

	async function finish(): Promise<void> {
		panel = null;
		await live.refetchAll().catch(() => {});
	}

	function startRename(a: Account): void {
		menu = null;
		confirming = null;
		renaming = a.id;
		renameValue = a.name;
		rowError[a.id] = '';
	}

	function cancelRename(a: Account): void {
		renaming = null;
		rowError[a.id] = '';
	}

	async function commitRename(a: Account): Promise<void> {
		const req = renameRequest(a, renameValue);
		if (req.kind === 'too_long') {
			rowError[a.id] = t('accounts.rename_too_long');
			return;
		}
		renaming = null;
		rowError[a.id] = '';
		if (req.kind === 'skip') return;
		try {
			await api.renameAccount(a.id, req.name);
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
		{#key panel}
			<AccountFlow
				bind:this={flowEl}
				bind:id={panel.id}
				bind:busy
				kind={panel.kind}
				existing={panel.isNew ? undefined : panel.id}
				onclose={() => (panel = null)}
				ondone={finish}
				onfail={(msg) => {
					addError = msg;
					panel = null;
				}}
			/>
		{/key}
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
						<div class="inline rename-row">
							<input
								class="inp rename"
								aria-label={t('accounts.rename_label', { name: a.name })}
								placeholder={a.platform_name}
								maxlength={NAME_MAX}
								bind:value={renameValue}
								use:focusSelect
								onkeydown={(e) => {
									if (e.key === 'Enter') void commitRename(a);
									else if (e.key === 'Escape') {
										e.stopPropagation();
										cancelRename(a);
									}
								}}
							/>
							<button type="button" class="btn primary" onclick={() => commitRename(a)}>{t('accounts.save')}</button>
							<button type="button" class="btn" onclick={() => cancelRename(a)}>{t('accounts.cancel')}</button>
						</div>
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
								<button type="button" role="menuitem" onclick={() => startRename(a)}>{t('accounts.rename')}</button>
								{#if !acts.includes('login')}
									<button type="button" role="menuitem" disabled={busy} onclick={() => openFlow(a.platform, a.id)}>{t('accounts.log_in_again')}</button>
								{/if}
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
	.rename-row {
		gap: 6px;
	}
	.rename-row .btn {
		height: 28px;
	}
	.rename {
		flex: 1;
		min-width: 0;
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
	.panel :global(.flow + .row),
	.panel :global(.row + .flow) {
		border-top: 1px solid var(--border-soft);
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
