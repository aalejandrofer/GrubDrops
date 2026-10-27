<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { api } from '$lib/app';
	import type { Settings, SettingsPatch } from '$lib/api/types';
	import PageHead from '$lib/components/PageHead.svelte';
	import { t } from '$lib/i18n/index.svelte';
	import { applyPrefs } from '$lib/prefs.svelte';
	import { SECTIONS, sectionFromHash, type Section } from '$lib/settings';
	import Accounts from './Accounts.svelte';
	import Engine from './Engine.svelte';
	import General from './General.svelte';
	import Health from './Health.svelte';
	import Notifications from './Notifications.svelte';
	import Password from './Password.svelte';
	import Updates from './Updates.svelte';

	const LABELS = {
		general: 'settings.general',
		accounts: 'settings.accounts',
		engine: 'settings.engine',
		notifications: 'settings.notifications',
		health: 'settings.health',
		updates: 'settings.updates',
		password: 'settings.password'
	} as const;

	let s = $state<Settings | null>(null);
	let errs = $state<Record<string, boolean>>({});
	let active = $state<Section>(sectionFromHash(typeof location === 'undefined' ? '' : location.hash));

	/** Applies the change at once, saves only that field, then takes the server body. */
	async function save(key: string, patch: SettingsPatch): Promise<boolean> {
		if (!s) return false;
		const prev = s;
		errs[key] = false;
		const { discord_webhook: hook, telegram_bot_token: tok, telegram_chat_id: chat, notify, ...plain } = patch;
		if (hook === undefined && tok === undefined && chat === undefined) {
			s = { ...s, ...plain, notify: { ...s.notify, ...notify } } as Settings;
			applyPrefs(s);
		}
		try {
			const body = await api.saveSettings(patch);
			s = body;
			applyPrefs(body);
			return true;
		} catch {
			s = prev;
			applyPrefs(prev);
			errs[key] = true;
			return false;
		}
	}

	/** A section link click or a hash on load wins over scroll tracking while the jump settles. */
	let holdUntil = 0;

	onMount(() => {
		api
			.settings()
			.then(async (b) => {
				s = b;
				await tick();
				if (location.hash) holdUntil = Date.now() + 800;
				if (location.hash) document.getElementById(location.hash.slice(1))?.scrollIntoView();
			})
			.catch(() => {});
	});

	const loaded = $derived(s !== null);

	/** The last section whose top has passed the line under the top bar, or the last one at the page bottom. */
	function track(): void {
		if (Date.now() < holdUntil) return;
		if (window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 2) {
			active = SECTIONS[SECTIONS.length - 1];
			return;
		}
		let cur: Section = 'general';
		for (const id of SECTIONS) {
			const el = document.getElementById(id);
			if (el && el.getBoundingClientRect().top <= 120) cur = id;
		}
		active = cur;
	}

	function onHash(): void {
		active = sectionFromHash(location.hash);
		holdUntil = Date.now() + 800;
	}

	$effect(() => {
		if (!loaded) return;
		const io = new IntersectionObserver(track, { rootMargin: '-120px 0px 0px 0px' });
		for (const id of SECTIONS) {
			const el = document.getElementById(id);
			if (el) io.observe(el);
		}
		return () => io.disconnect();
	});
</script>

<svelte:window onhashchange={onHash} onscroll={track} />

<PageHead title={t('nav.settings')} stamp={t('settings.stamp')}>
	<div class="settings layout">
		<nav class="subnav" aria-label={t('settings.nav_label')}>
			{#each SECTIONS as id (id)}
				<a href="#{id}" class:on={active === id} aria-current={active === id ? 'true' : undefined}>{t(LABELS[id])}</a>
			{/each}
		</nav>
		<div>
			{#if s}<General {s} {save} {errs} />{:else}<section class="set" id="general"></section>{/if}
			<Accounts />
			{#if s}<Engine {s} {save} {errs} />{:else}<section class="set" id="engine"></section>{/if}
			{#if s}<Notifications {s} {save} {errs} />{:else}<section class="set" id="notifications"></section>{/if}
			{#if s}<Health {s} {save} {errs} />{:else}<Health />{/if}
			{#if s}<Updates {s} {save} {errs} />{:else}<section class="set" id="updates"></section>{/if}
				<Password />
		</div>
	</div>
</PageHead>

<style>
	.layout {
		display: grid;
		grid-template-columns: 180px minmax(0, 1fr);
		gap: 40px;
		align-items: start;
	}
	.subnav {
		position: sticky;
		top: 76px;
		display: grid;
		gap: 2px;
	}
	.subnav a {
		display: flex;
		align-items: center;
		padding: 6px 10px;
		border-radius: 6px;
		color: var(--t2);
		font-size: 13.5px;
		font-weight: 500;
	}
	.subnav a:hover {
		color: var(--text);
		background: var(--surface);
	}
	.subnav a.on {
		color: var(--text);
		background: var(--surface-2);
		box-shadow: inset 2px 0 0 var(--accent);
	}
	.subnav a:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}

	.settings :global(section.set) {
		scroll-margin-top: 72px;
		padding-bottom: 36px;
		margin-bottom: 36px;
		border-bottom: 1px solid var(--border-soft);
	}
	.settings :global(section.set:last-child) {
		border-bottom: 0;
		margin-bottom: 0;
	}
	.settings :global(.sec-head) {
		display: flex;
		align-items: flex-end;
		justify-content: space-between;
		gap: 12px;
		flex-wrap: wrap;
		margin-bottom: 14px;
	}
	.settings :global(.sec-head h2) {
		margin: 0;
		font-size: 15px;
		font-weight: 600;
		letter-spacing: -0.01em;
	}
	.settings :global(.sec-head p) {
		margin: 2px 0 0;
		color: var(--t2);
		font-size: 13px;
	}
	.settings :global(.panel) {
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
	}
	.settings :global(.row) {
		display: flex;
		align-items: center;
		gap: 14px;
		padding: 12px 16px;
		min-width: 0;
	}
	.settings :global(.row + .row) {
		border-top: 1px solid var(--border-soft);
	}
	.settings :global(.lbl) {
		flex: 1;
		min-width: 0;
	}
	.settings :global(.lbl b) {
		display: block;
		font-weight: 500;
		font-size: 13.5px;
	}
	.settings :global(.lbl small) {
		display: block;
		color: var(--t2);
		font-size: 12.5px;
	}
	.settings :global(.lbl small.bad),
	.settings :global(.feedback.bad) {
		color: var(--red);
	}
	.settings :global(.btn) {
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
		cursor: pointer;
	}
	.settings :global(.btn:hover:not(:disabled)) {
		border-color: var(--t2);
	}
	.settings :global(.btn:disabled) {
		opacity: 0.6;
		cursor: default;
	}
	.settings :global(.btn:focus-visible),
	.settings :global(.sw:focus-visible),
	.settings :global(.seg button:focus-visible) {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.settings :global(.sel) {
		height: 32px;
		padding: 0 30px 0 10px;
		border: 1px solid var(--border);
		border-radius: 7px;
		background: var(--surface-2)
			url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%23a3acb7' stroke-width='2' stroke-linecap='round'%3E%3Cpath d='m6 9 6 6 6-6'/%3E%3C/svg%3E")
			no-repeat right 9px center / 14px;
		color: var(--text);
		font: 13px var(--font);
		appearance: none;
		min-width: 0;
	}
	.settings :global(.sel:focus) {
		outline: 0;
		border-color: var(--accent);
		box-shadow: 0 0 0 3px var(--accent-soft);
	}
	.settings :global(.seg) {
		display: inline-flex;
		padding: 2px;
		border: 1px solid var(--border);
		border-radius: 8px;
		background: var(--bg);
		flex: none;
	}
	.settings :global(.seg button) {
		border: 0;
		background: none;
		padding: 4px 12px;
		border-radius: 6px;
		font: inherit;
		font-size: 13px;
		font-weight: 500;
		color: var(--t2);
		cursor: pointer;
	}
	.settings :global(.seg button[aria-pressed='true']) {
		background: var(--surface-3);
		color: var(--text);
	}
	.settings :global(.sw) {
		position: relative;
		width: 34px;
		height: 20px;
		border-radius: 10px;
		border: 1px solid var(--border);
		background: var(--surface-3);
		flex: none;
		padding: 0;
		cursor: pointer;
		transition:
			background 0.15s,
			border-color 0.15s;
	}
	.settings :global(.sw::after) {
		content: '';
		position: absolute;
		top: 2px;
		left: 2px;
		width: 14px;
		height: 14px;
		border-radius: 50%;
		background: var(--t2);
		transition:
			transform 0.15s,
			background 0.15s;
	}
	.settings :global(.sw[aria-checked='true']) {
		background: var(--accent-strong);
		border-color: var(--accent-strong);
	}
	.settings :global(.sw[aria-checked='true']::after) {
		transform: translateX(14px);
		background: #fff;
	}
	.settings :global(.field) {
		display: grid;
		gap: 6px;
	}
	.settings :global(.field label) {
		font-size: 12.5px;
		font-weight: 500;
		color: var(--t2);
	}
	.settings :global(.inp) {
		height: 34px;
		padding: 0 10px;
		border: 1px solid var(--border);
		border-radius: 7px;
		background: var(--bg);
		color: var(--text);
		font: 12.5px var(--mono);
		min-width: 0;
		width: 100%;
	}
	.settings :global(.inp:focus) {
		outline: 0;
		border-color: var(--accent);
		box-shadow: 0 0 0 3px var(--accent-soft);
	}
	.settings :global(.inp:disabled) {
		color: var(--t3);
		background: var(--surface);
		cursor: not-allowed;
	}
	.settings :global(.inp::placeholder) {
		color: var(--t3);
	}
	.settings :global(.inline) {
		display: flex;
		gap: 8px;
		align-items: center;
		min-width: 0;
	}
	.settings :global(.card-body) {
		padding: 14px 16px;
		display: grid;
		gap: 12px;
	}
	.settings :global(.card-body + .card-body) {
		border-top: 1px solid var(--border-soft);
	}
	.settings :global(.card-title) {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
	}
	.settings :global(.card-title b) {
		font-size: 13.5px;
		font-weight: 500;
	}
	.settings :global(.tag) {
		font-size: 12px;
		padding: 1px 7px;
		border-radius: 10px;
		color: var(--t2);
		background: var(--surface-3);
	}
	.settings :global(.tag.ok) {
		color: var(--green);
		background: var(--green-soft);
	}
	.settings :global(.tag.soon) {
		color: var(--accent);
		background: var(--accent-soft);
	}
	.settings :global(.two) {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 12px;
	}
	.settings :global(.feedback) {
		font-size: 12px;
		color: var(--green);
	}
	.settings :global(.sub-h) {
		margin: 18px 0 8px;
		font-size: 12.5px;
		font-weight: 600;
		color: var(--t2);
	}
	.settings :global(.row + .card-body) {
		border-top: 1px solid var(--border-soft);
	}
	.settings :global(details.adv) {
		margin-top: 18px;
	}
	.settings :global(details.adv > summary) {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		margin-bottom: 8px;
		font-size: 12.5px;
		font-weight: 600;
		color: var(--t2);
		list-style: none;
		cursor: pointer;
		border-radius: 4px;
	}
	.settings :global(details.adv > summary::-webkit-details-marker) {
		display: none;
	}
	.settings :global(details.adv > summary::before) {
		content: '';
		width: 6px;
		height: 6px;
		border-right: 1.5px solid currentColor;
		border-bottom: 1.5px solid currentColor;
		transform: rotate(-45deg);
		transition: transform 0.15s;
	}
	.settings :global(details.adv[open] > summary::before) {
		transform: rotate(45deg);
	}
	.settings :global(details.adv > summary:hover) {
		color: var(--text);
	}
	.settings :global(details.adv > summary:focus-visible) {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.settings :global(.sw:disabled) {
		opacity: 0.45;
		cursor: default;
	}

	@media (max-width: 720px) {
		.layout {
			grid-template-columns: minmax(0, 1fr);
			gap: 16px;
		}
		.subnav {
			top: 52px;
			z-index: 12;
			display: flex;
			overflow-x: auto;
			scrollbar-width: none;
			margin: 0 -16px;
			padding: 8px 16px;
			background: color-mix(in srgb, var(--bg) 95%, transparent);
			border-bottom: 1px solid var(--border-soft);
		}
		.subnav::-webkit-scrollbar {
			display: none;
		}
		.subnav a {
			flex: none;
			padding: 5px 10px;
		}
		.subnav a.on {
			box-shadow: inset 0 -2px 0 var(--accent);
		}
		.settings :global(section.set) {
			scroll-margin-top: 112px;
		}
		.settings :global(.row) {
			flex-wrap: wrap;
			padding: 12px 14px;
		}
		.settings :global(.two) {
			grid-template-columns: 1fr;
		}
	}
	@media (max-width: 420px) {
		.settings :global(.inline.wrap) {
			flex-wrap: wrap;
		}
		.settings :global(.inline.wrap .btn) {
			width: 100%;
			justify-content: center;
		}
	}
</style>
