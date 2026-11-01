<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api } from '$lib/app';
	import type { Account, Platform } from '$lib/api/types';
	import { STATUS_KEYS } from '$lib/account-panel';
	import { accountLine, accountStatus, commitAccountOrder, initials, rowActions } from '$lib/accounts';
	import AddAccount from '$lib/components/AddAccount.svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { moveTarget } from '$lib/games';
	import { t, tn } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';
	import { SECTION_LOOK, needLoginCount } from '$lib/settings-look';
	import { moveItem } from '$lib/upnext';
	import { reorder } from '$lib/drag';
	import { AccountActions } from './account-actions.svelte';
	import AccountMenu from './AccountMenu.svelte';
	import AccountPanel from './AccountPanel.svelte';
	import SectionHead from './SectionHead.svelte';

	const live = getLive();
	const act = new AccountActions(live);

	// New accounts sign in through the dialog; its half-made account stays out of the rows.
	let adding = $state<Platform | null>(null);
	let addId = $state('');
	let dlg = $state<HTMLDialogElement | null>(null);
	// A sign-in flow in the drawer is creating or saving.
	let busy = $state(false);
	let addError = $state('');

	const runnerOf = (id: string) => live.runners.find((r) => r.account_id === id);
	const shown = (a: Account) => !(adding && addId === a.id);

	let ordering = $state(false);
	let orderError = $state('');
	let drag = $state<{ from: number; to: number } | null>(null);

	// A half-made account stays out of the rows and last in the saved order.
	const listed = $derived(live.accounts.filter(shown));
	const rows = $derived(drag ? moveItem(listed, drag.from, drag.to) : listed);
	const needLogin = $derived(needLoginCount(listed, live.runners));

	// A removed account closes its drawer.
	$effect(() => {
		if (act.open && !live.accounts.some((a) => a.id === act.open)) act.close();
	});

	async function commitOrder(next: Account[]): Promise<void> {
		if (ordering) return;
		ordering = true;
		orderError = '';
		const ok = await commitAccountOrder(
			{
				get: () => live.accounts,
				set: (a) => (live.accounts = a),
				setOrder: (ids) => api.setAccountOrder(ids),
				fetchAccounts: () => api.accounts()
			},
			[...next, ...live.accounts.filter((a) => !shown(a))]
		);
		ordering = false;
		if (!ok) orderError = t('accounts.reorder_failed');
		else void live.refetchAll().catch(() => {});
	}

	function step(i: number, dir: -1 | 1, e: MouseEvent): void {
		act.menu = null;
		void commitOrder(moveItem(listed, i, moveTarget(listed.length, i, dir, e.shiftKey)));
	}

	const dragOpts = {
		items: (list: HTMLElement) => Array.from(list.querySelectorAll<HTMLElement>(':scope > .acct')),
		enabled: () => !ordering,
		onStart: (from: number) => (drag = { from, to: from }),
		onMove: (to: number) => {
			if (drag) drag.to = to;
		},
		onEnd: (from: number, to: number) => {
			drag = null;
			if (from !== to) void commitOrder(moveItem(listed, from, to));
		},
		onCancel: () => (drag = null)
	};

	function openAdd(kind: Platform = 'twitch'): void {
		if (busy) return;
		act.menu = null;
		act.close();
		addError = '';
		addId = '';
		adding = kind;
		dlg?.showModal();
	}

	async function added(): Promise<void> {
		dlg?.close();
		await live.refetchAll().catch(() => {});
	}

	// A click on the row itself opens its drawer; its buttons and the grip do their own thing.
	function onRowClick(e: MouseEvent, a: Account): void {
		if (e.target instanceof Element && e.target.closest('button, a, input, .grip, .menu-wrap')) return;
		act.openPanel(a.id);
	}

	function onWindowClick(e: MouseEvent): void {
		// composedPath still lists a clicked button the click handler has already replaced.
		const inside = e.composedPath().some((n) => n instanceof Element && n.classList.contains('menu-wrap'));
		if (act.menu && !inside) act.menu = null;
	}

	// An open ⋯ menu takes Escape before the drawer does.
	function onEscape(e: KeyboardEvent): void {
		if (e.key !== 'Escape' || !act.menu) return;
		act.menu = null;
		e.preventDefault();
	}

	onMount(() => {
		const add = new URL(location.href).searchParams.get('add');
		if (add === 'twitch' || add === 'kick') {
			void goto(location.pathname + location.hash, { replace: true });
			openAdd(add);
		}
	});
</script>

<svelte:window onclick={onWindowClick} onkeydowncapture={onEscape} />

<section class="set hue-{SECTION_LOOK.accounts.hue}" id="accounts" aria-labelledby="h-acc">
	<SectionHead sec="accounts" id="h-acc" blurb={t('accounts.intro')}>
		{#snippet chips()}
			{#if needLogin > 0}<span class="chip warn">{tn('settings.chip_need_login', needLogin)}</span>{/if}
		{/snippet}
		{#snippet right()}
			<button type="button" class="btn tint" disabled={busy} onclick={() => openAdd()}>
				<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>{t('accounts.add')}
			</button>
		{/snippet}
	</SectionHead>
	{#if addError}<p class="bad top" role="alert">{addError}</p>{/if}
	{#if orderError}<p class="bad top" role="alert">{orderError}</p>{/if}
	<div class="panel" id="acct-list" class:none={listed.length === 0} use:reorder={dragOpts}>
		{#each rows as a, i (a.id)}
			{@const r = runnerOf(a.id)}
			{@const st = accountStatus(a, r)}
			{@const acts = rowActions(a, r)}
			{@const on = act.open === a.id}
			{@const n = on ? undefined : act.rowNote(a.id)}
			<!-- The row's open button is the keyboard path; a click anywhere else on the row is a shortcut. -->
			<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
			<div
				class="row acct"
				class:problem={st === 'signed_out'}
				class:on
				class:dragging={drag && drag.to === i}
				data-acct={a.id}
				onclick={(e) => onRowClick(e, a)}
			>
				<div class="grip">
					<button type="button" aria-label={t('accounts.move_up')} title={t('accounts.move_up')} disabled={i === 0 || ordering} onclick={(e) => step(i, -1, e)}>
						<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 15 6-6 6 6" /></svg>
					</button>
					<span class="h" title={t('accounts.drag')} role="presentation">
						<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M9 6h.01M15 6h.01M9 12h.01M15 12h.01M9 18h.01M15 18h.01" stroke-width="3" /></svg>
					</span>
					<button type="button" aria-label={t('accounts.move_down')} title={t('accounts.move_down')} disabled={i === rows.length - 1 || ordering} onclick={(e) => step(i, 1, e)}>
						<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg>
					</button>
				</div>
				<span class="av {a.platform}">
					{#if a.avatar_url}<img src={a.avatar_url} alt={t('accounts.avatar', { name: a.name })} />{:else}{initials(a.name)}{/if}
					<span class="mk"><PlatformMark platform={a.platform} /></span>
				</span>
				<button type="button" class="who" aria-label={t('accounts.open', { name: a.name })} onclick={() => act.openPanel(a.id)}>
					<b class="pf-name {a.platform}">{a.name}</b>
					<small>{accountLine(a, r)}</small>
				</button>
				{#if n}<small class="note {n.tone}" role={n.tone === 'bad' ? 'alert' : 'status'}>{n.text}</small>{/if}
				<span class="st {st}">{t(STATUS_KEYS[st])}</span>
				{#if acts.includes('login')}
					<button type="button" class="btn primary fix" disabled={busy} onclick={() => act.startLogin(a)}>
						<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4M10 17l5-5-5-5M15 12H3" /></svg>{t('accounts.log_in_again')}
					</button>
				{/if}
				{#if acts.includes('resume')}
					<button type="button" class="btn fix" onclick={() => act.setEnabled(a, true)}>{t('accounts.resume')}</button>
				{/if}
				<AccountMenu {act} {a} />
				<button type="button" class="ico chev" aria-label={t('accounts.open', { name: a.name })} tabindex="-1" onclick={() => act.openPanel(a.id)}>
					<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="m9 6 6 6-6 6" /></svg>
				</button>
			</div>
		{/each}
	</div>
	<AccountPanel {act} bind:busy />
</section>

<dialog
	class="add"
	bind:this={dlg}
	aria-labelledby="add-h"
	onclose={() => ((adding = null), (addId = ''))}
	onclick={(e) => e.target === dlg && dlg?.close()}
>
	{#if adding}
		<div class="add-head">
			<h2 id="add-h">{t('welcome.account_title')}</h2>
			<button type="button" class="ico" aria-label={t('accounts.cancel')} onclick={() => dlg?.close()}>
				<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12" /></svg>
			</button>
		</div>
		<AddAccount
			platform={adding}
			bind:id={addId}
			onadded={added}
			onpick={() => (addError = '')}
			onfail={(msg) => {
				addError = msg;
				dlg?.close();
			}}
		/>
	{/if}
</dialog>

<style>
	.add {
		width: min(560px, calc(100vw - 32px));
		max-height: calc(100dvh - 32px);
		padding: 20px;
		border: 1px solid var(--border);
		border-radius: 12px;
		background: var(--bg);
		color: var(--text);
	}
	.add::backdrop {
		background: rgb(0 0 0 / 0.55);
	}
	.add-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		margin-bottom: 16px;
	}
	.add-head h2 {
		margin: 0;
		font-size: 17px;
		font-weight: 600;
	}
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
	.i,
	#accounts :global(.i) {
		width: 14px;
		height: 14px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.8;
		stroke-linecap: round;
		stroke-linejoin: round;
		flex: none;
	}
	.ico,
	#accounts :global(.ico) {
		width: 30px;
		height: 30px;
		display: grid;
		place-items: center;
		flex: none;
		border: 0;
		border-radius: 6px;
		background: none;
		color: var(--t2);
		cursor: pointer;
	}
	.ico:hover,
	#accounts :global(.ico:hover:not(:disabled)) {
		background: var(--surface-3);
		color: var(--text);
	}
	.ico:focus-visible,
	#accounts :global(.ico:focus-visible),
	#accounts :global(.lnk:focus-visible) {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	#accounts :global(.btn.primary),
	.add :global(.btn.primary) {
		background: var(--accent-strong);
		border-color: var(--accent-strong);
		color: #fff;
	}
	#accounts :global(.btn.primary:hover:not(:disabled)),
	.add :global(.btn.primary:hover:not(:disabled)) {
		background: color-mix(in srgb, var(--accent-strong) 85%, #fff);
		border-color: color-mix(in srgb, var(--accent-strong) 85%, #fff);
	}
	#accounts :global(.av) {
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
	#accounts :global(.av.twitch) {
		color: var(--twitch-av);
		background: color-mix(in srgb, var(--twitch) 18%, transparent);
	}
	#accounts :global(.av.kick) {
		color: var(--kick-fg);
		background: color-mix(in srgb, var(--kick) 14%, transparent);
	}
	#accounts :global(.av img) {
		width: 100%;
		height: 100%;
		border-radius: 50%;
		object-fit: cover;
	}
	#accounts :global(.av .mk) {
		position: absolute;
		right: -3px;
		bottom: -3px;
		display: grid;
		border-radius: 4px;
		box-shadow: 0 0 0 2px var(--surface);
	}
	#accounts :global(.st) {
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
	#accounts :global(.st::before) {
		content: '';
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: currentColor;
	}
	#accounts :global(.st.mining),
	#accounts :global(.st.force_watching) {
		color: var(--green);
		background: var(--green-soft);
	}
	#accounts :global(.st.sleep_window) {
		color: var(--accent);
		background: var(--accent-soft);
	}
	#accounts :global(.st.signed_out) {
		color: var(--red);
		background: var(--red-soft);
	}
	#accounts :global(.st.paused) {
		color: var(--amber);
		background: var(--amber-soft);
	}
	#accounts :global(.lnk) {
		border: 0;
		background: none;
		padding: 0;
		font: inherit;
		color: var(--accent);
		text-decoration: underline;
		cursor: pointer;
	}
	#accounts :global(a.lnk) {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		font-size: 13px;
		justify-self: start;
	}
	#accounts :global(.note) {
		font-size: 12.5px;
		margin: 0;
	}
	#accounts :global(.note.ok) {
		color: var(--green);
	}
	#accounts :global(.note.wait) {
		color: var(--t2);
	}
	#accounts :global(.note.bad) {
		color: var(--red);
	}
	#accounts :global(.mono) {
		font-family: var(--mono);
		font-variant-numeric: tabular-nums;
	}
	/* Drawer sections. */
	#accounts :global(.sec) {
		display: grid;
		gap: 12px;
		min-width: 0;
	}
	#accounts :global(.sh) {
		margin: 0;
		font-size: 11px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--t3);
	}
	#accounts :global(.opt) {
		display: flex;
		align-items: center;
		gap: 12px;
		min-width: 0;
	}
	#accounts :global(.opt .lbl b) {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
	}
	#accounts :global(.lbl .lnk) {
		font-size: 12.5px;
	}
	#accounts :global(.kv) {
		display: grid;
		grid-template-columns: max-content minmax(0, 1fr);
		gap: 6px 16px;
		margin: 0;
		font-size: 13px;
	}
	#accounts :global(.kv dt) {
		color: var(--t3);
	}
	#accounts :global(.kv dd) {
		margin: 0;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	#accounts :global(.acts) {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
	.acct {
		cursor: pointer;
	}
	.acct.on {
		background: var(--surface-2);
	}
	.who {
		flex: 1;
		min-width: 0;
		border: 0;
		background: none;
		padding: 0;
		font: inherit;
		color: inherit;
		text-align: left;
		cursor: pointer;
	}
	.who b {
		display: block;
		font-weight: 500;
		font-size: 13.5px;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.who:hover b {
		text-decoration: underline;
		text-underline-offset: 3px;
	}
	.who small {
		display: block;
		color: var(--t2);
		font-size: 12.5px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.who:focus-visible {
		outline: 2px solid var(--hue);
		outline-offset: 2px;
		border-radius: 4px;
	}
	.row > .note {
		font-size: 12px;
		max-width: 40%;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.acct.dragging {
		background: var(--surface-2);
		box-shadow: 0 8px 24px rgb(0 0 0 / 0.35);
		position: relative;
		z-index: 1;
	}
	.grip {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 2px;
		color: var(--t3);
		flex: none;
		cursor: default;
	}
	.h {
		cursor: grab;
		padding: 2px 0;
		touch-action: none;
		display: grid;
	}
	.grip button {
		width: 26px;
		height: 20px;
		display: grid;
		place-items: center;
		border: 0;
		background: none;
		border-radius: 4px;
		color: var(--t2);
		cursor: pointer;
	}
	.grip button:hover:not(:disabled) {
		background: var(--surface-3);
		color: var(--text);
	}
	.grip button:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.grip button:disabled {
		opacity: 0.3;
		cursor: default;
	}
	.acct.problem {
		background: linear-gradient(90deg, color-mix(in srgb, var(--red) 7%, transparent), transparent 60%);
	}
	.acct.problem .who small {
		color: color-mix(in srgb, var(--red) 55%, var(--text));
	}

	@media (max-width: 520px) {
		#accounts :global(.kv) {
			grid-template-columns: minmax(0, 1fr);
			gap: 0;
		}
		#accounts :global(.kv dd + dt) {
			margin-top: 8px;
		}
		#accounts :global(.opt) {
			flex-wrap: wrap;
		}
		#accounts :global(.opt > .lbl) {
			flex: 1 1 180px;
		}
		#accounts :global(.opt > .btn),
		#accounts :global(.opt > .acts) {
			margin-left: auto;
		}
	}
	@media (max-width: 720px) {
		.acct {
			gap: 10px 12px;
		}
		.acct .who {
			flex: 1 1 calc(100% - 180px);
		}
		.acct :global(.menu-wrap) {
			order: 2;
		}
		.acct .chev {
			order: 3;
		}
		.acct .st {
			order: 4;
			margin-left: 76px;
		}
		.row > .note {
			order: 5;
			flex-basis: 100%;
			max-width: none;
			margin-left: 76px;
			white-space: normal;
		}
		.acct .fix {
			order: 6;
			width: 100%;
			justify-content: center;
		}
	}
</style>
