<script lang="ts">
	import { onDestroy, onMount, tick } from 'svelte';
	import { beforeNavigate, replaceState } from '$app/navigation';
	import { advanced, syncAdvanced } from '$lib/advanced.svelte';
	import { api } from '$lib/app';
	import type { Platform, Settings, SettingsPatch } from '$lib/api/types';
	import { Autosave, type Phase } from '$lib/autosave';
	import CountdownToast from '$lib/components/CountdownToast.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import PageHead from '$lib/components/PageHead.svelte';
	import { t } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';
	import { applyPrefs } from '$lib/prefs.svelte';
	import { SECTIONS, sectionFromHash, type Section } from '$lib/settings';
	import { SECTION_LOOK, navDots, needLoginCount } from '$lib/settings-look';
	import Accounts from './Accounts.svelte';
	import Engine from './Engine.svelte';
	import Experimental from './Experimental.svelte';
	import General from './General.svelte';
	import Health from './Health.svelte';
	import Notifications from './Notifications.svelte';
	import Password from './Password.svelte';
	import Updates from './Updates.svelte';

	const live = getLive();
	/** Failing canary platforms, reported up by the Health section. */
	let canaryFailing = $state<Platform[]>([]);
	const dots = $derived(navDots({ needLogin: needLoginCount(live.accounts, live.runners), canaryFailing }));
	/** Dots are colour only, so screen readers get the state as text. */
	const DOT_TEXT = { warn: 'settings.dot_warn', bad: 'settings.dot_bad' } as const;

	let s = $state<Settings | null>(null);
	let errs = $state<Record<string, boolean>>({});
	let active = $state<Section>(sectionFromHash(typeof location === 'undefined' ? '' : location.hash));

	/** Applies the change at once, saves only that field, then takes the server body. */
	async function save(key: string, patch: SettingsPatch): Promise<boolean> {
		if (!s) return false;
		const prev = s;
		errs[key] = false;
		const { discord_webhook: hook, telegram_bot_token: tok, telegram_chat_id: chat, notify, sleep, ...plain } = patch;
		if (hook === undefined && tok === undefined && chat === undefined) {
			s = { ...s, ...plain, notify: { ...s.notify, ...notify }, sleep: { ...s.sleep, ...sleep } } as Settings;
			applyPrefs(s);
		}
		try {
			const body = await api.saveSettings(patch, leaving);
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

	// Typed fields save 10s after the last edit; the toast counts down.
	let autoPhase = $state<Phase>('idle');
	let autoNow = $state(Date.now());
	const auto = new Autosave(() => {
		autoPhase = auto.phase;
		autoNow = Date.now();
	});
	const autoText = $derived(
		autoPhase === 'wait'
			? t('settings.autosave_in', { n: auto.secondsLeft(autoNow) })
			: autoPhase === 'saving'
				? t('accounts.saving_now')
				: autoPhase === 'saved'
					? t('accounts.saved')
					: null
	);
	$effect(() => {
		if (autoPhase !== 'wait') return;
		const id = setInterval(() => (autoNow = Date.now()), 250);
		return () => clearInterval(id);
	});

	/** True while the page unloads, so saves go out with keepalive. */
	let leaving = false;
	function flushNow(unload: boolean): void {
		leaving = unload;
		void auto.flush(unload);
		leaving = false;
	}

	// Leaving with pending edits saves them at once. Edits that can't save ask first.
	beforeNavigate((nav) => {
		if (!auto.has()) return;
		if (auto.invalid().length > 0) {
			if (nav.willUnload) nav.cancel();
			else if (!confirm(t('settings.autosave_leave'))) {
				nav.cancel();
				return;
			}
		}
		flushNow(nav.willUnload);
	});
	onDestroy(() => auto.dispose());

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

	/** The column that holds every section, so the Advanced pref reaches each one. */
	let body = $state<HTMLElement>();
	$effect(() => {
		if (body) return syncAdvanced(body, advanced.expand);
	});

	/** The last section whose top has passed the line under the top bar, or the last one at the page bottom. */
	function track(): void {
		if (Date.now() < holdUntil) return;
		if (window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 2) {
			active = SECTIONS[SECTIONS.length - 1];
			return;
		}
		// Just under where a nav jump lands a section (its scroll-margin-top), plus some slack.
		const line = (window.matchMedia('(max-width: 720px)').matches ? 140 : 92) + 40;
		let cur: Section = 'general';
		for (const id of SECTIONS) {
			const el = document.getElementById(id);
			if (el && el.getBoundingClientRect().top <= line) cur = id;
		}
		active = cur;
	}

	function jump(e: MouseEvent, id: Section): void {
		e.preventDefault();
		active = id;
		// Hold the picked link through the smooth scroll; scrollend releases it early.
		holdUntil = Date.now() + 2000;
		replaceState(`#${id}`, {});
		document.getElementById(id)?.scrollIntoView({ behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
	}

	/** Phone: keep the active pill in view as the page scrolls. */
	$effect(() => {
		const el = document.getElementById(`sn-${active}`);
		const row = el?.parentElement;
		if (!el || !row || row.scrollWidth <= row.clientWidth) return;
		row.scrollTo({ left: el.offsetLeft - row.clientWidth / 2 + el.offsetWidth / 2, behavior: 'smooth' });
	});

	/** A jump's scroll has landed: drop the hold and read the page again. */
	function settle(): void {
		if (holdUntil === 0) return;
		holdUntil = 0;
		track();
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

<svelte:window onhashchange={onHash} onscroll={track} onscrollend={settle} onpagehide={() => auto.has() && flushNow(true)} />

<PageHead title={t('nav.settings')} stamp={t('settings.stamp')}>
	<div class="settings layout">
		<nav class="subnav" aria-label={t('settings.nav_label')}>
			{#each SECTIONS as id (id)}
				{@const look = SECTION_LOOK[id]}
				<a
					id="sn-{id}"
					href="#{id}"
					class="hue-{look.hue}"
					class:on={active === id}
					aria-current={active === id ? 'true' : undefined}
					onclick={(e) => jump(e, id)}
				>
					<span class="tile sm" aria-hidden="true"><Icon name={look.icon} size={13} /></span>
					<span class="nm">{t(look.label)}</span>
					{#if dots[id]}{@const d = dots[id]}<span class="dot {d}" aria-hidden="true"></span><span class="vh">, {t(DOT_TEXT[d])}</span>{/if}
				</a>
			{/each}
		</nav>
		<div bind:this={body}>
			{#if s}<General {s} {save} {errs} />{:else}<section class="set" id="general"></section>{/if}
			<Accounts />
			{#if s}<Engine {s} {save} {errs} {auto} />{:else}<section class="set" id="engine"></section>{/if}
			{#if s}<Experimental {s} {save} {errs} />{:else}<section class="set" id="experimental"></section>{/if}
			{#if s}<Notifications {s} {save} {errs} {auto} />{:else}<section class="set" id="notifications"></section>{/if}
			{#if s}<Health {s} {save} {errs} bind:failing={canaryFailing} />{:else}<Health bind:failing={canaryFailing} />{/if}
			{#if s}<Updates {s} {save} {errs} />{:else}<section class="set" id="updates"></section>{/if}
			{#if s}<Password {s} {save} {errs} />{:else}<Password />{/if}
		</div>
		{#if autoText}<CountdownToast text={autoText} />{/if}
	</div>
</PageHead>

<style>
	.layout {
		display: grid;
		grid-template-columns: 196px minmax(0, 820px);
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
		gap: 10px;
		padding: 5px 8px;
		border-radius: 8px;
		color: var(--t2);
		font-size: 13.5px;
		font-weight: 500;
		transition:
			background 0.15s,
			color 0.15s;
	}
	.subnav a:hover {
		color: var(--text);
		background: var(--surface);
	}
	.subnav a.on {
		color: var(--text);
		font-weight: 600;
		background: color-mix(in srgb, var(--hue) 16%, transparent);
	}
	.subnav a:focus-visible {
		outline: 2px solid var(--hue);
		outline-offset: 2px;
	}
	.nm {
		flex: 1;
	}
	.dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		flex: none;
	}
	.dot.warn {
		background: var(--amber);
	}
	.dot.bad {
		background: var(--red);
	}
	/* Text only screen readers announce. */
	.settings :global(.vh) {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		clip-path: inset(50%);
		white-space: nowrap;
	}

	/* Every section, its nav link and its controls read --hue (text) and --fill (solid controls); accent until a hue class sets them. */
	.settings {
		--hue: var(--accent);
		--fill: var(--hue);
		/* Phone: the save toast sits under the top bar (52px) and the sticky pill row (57px). */
		--toast-top-phone: 117px;
	}
	.settings :global(.tile) {
		display: inline-grid;
		place-items: center;
		flex: none;
		width: 30px;
		height: 30px;
		border-radius: 8px;
		color: var(--hue);
		background: color-mix(in srgb, var(--hue) var(--soft), transparent);
	}
	.settings :global(.tile.sm) {
		width: 22px;
		height: 22px;
		border-radius: 6px;
	}
	.settings :global(.chip) {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		font-size: 12px;
		font-weight: 500;
		line-height: 18px;
		padding: 1px 8px;
		border-radius: 999px;
		color: var(--t2);
		background: var(--surface-3);
		white-space: nowrap;
	}
	.settings :global(.chip::before) {
		content: '';
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: currentColor;
	}
	.settings :global(.chip.ok) {
		color: var(--green);
		background: var(--green-soft);
	}
	.settings :global(.chip.bad) {
		color: var(--chip-bad);
		background: var(--red-soft);
	}
	.settings :global(.chip.warn) {
		color: var(--amber);
		background: var(--amber-soft);
	}
	.settings :global(.chip.info) {
		color: var(--blue);
		background: var(--blue-soft);
	}
	.settings :global(.chip.off) {
		color: var(--t3);
	}
	.settings :global(.chip.off::before) {
		background: transparent;
		box-shadow: inset 0 0 0 1.5px currentColor;
	}

	/* A nav jump lands a section 40px under the 52px top bar. */
	.settings :global(section.set) {
		scroll-margin-top: 92px;
		padding-bottom: 36px;
		margin-bottom: 36px;
		border-bottom: 1px solid var(--border-soft);
	}
	.settings :global(section.set:last-child) {
		border-bottom: 0;
		margin-bottom: 0;
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
	.settings :global(.btn.tint) {
		color: var(--hue);
		border-color: color-mix(in srgb, var(--hue) 45%, transparent);
		background: color-mix(in srgb, var(--hue) 10%, transparent);
	}
	.settings :global(.btn.tint:hover:not(:disabled)) {
		border-color: var(--hue);
	}
	.settings :global(.btn:focus-visible),
	.settings :global(.sw:focus-visible),
	.settings :global(.seg button:focus-visible),
	.settings :global(summary:focus-visible) {
		outline: 2px solid var(--hue);
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
		border-color: var(--hue);
		box-shadow: 0 0 0 3px color-mix(in srgb, var(--hue) 22%, transparent);
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
		background: color-mix(in srgb, var(--fill) 18%, var(--surface-2));
		color: var(--text);
		box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--fill) 55%, transparent);
	}
	.settings :global(.sw) {
		position: relative;
		width: 34px;
		height: 20px;
		border-radius: 10px;
		border: 1px solid var(--dim);
		background: var(--surface-3);
		flex: none;
		padding: 0;
		cursor: pointer;
		transition:
			background 0.15s,
			border-color 0.15s;
	}
	/* About a 44px tap area without a bigger switch. */
	.settings :global(.sw::before) {
		content: '';
		position: absolute;
		inset: -12px -8px;
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
		background: var(--fill);
		border-color: var(--fill);
	}
	.settings :global(.sw[aria-checked='true']::after) {
		transform: translateX(14px);
		background: var(--knob);
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
		border-color: var(--hue);
		box-shadow: 0 0 0 3px color-mix(in srgb, var(--hue) 22%, transparent);
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
		display: flex;
		align-items: center;
		gap: 8px;
		margin: 22px 0 8px;
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
	.settings :global(.sw:disabled) {
		opacity: 0.45;
		cursor: default;
	}

	@media (max-width: 720px) {
		.layout {
			grid-template-columns: minmax(0, 1fr);
			gap: 16px;
		}
		/* Phone: the index is a sticky row of pills under the top bar. */
		.subnav {
			top: 52px;
			z-index: 9;
			display: flex;
			gap: 6px;
			overflow-x: auto;
			scrollbar-width: none;
			margin: 0 -16px;
			padding: 10px 16px;
			background: color-mix(in srgb, var(--bg) 94%, transparent);
			backdrop-filter: blur(6px);
			border-bottom: 1px solid var(--border-soft);
		}
		.subnav::-webkit-scrollbar {
			display: none;
		}
		.subnav a {
			flex: none;
			gap: 7px;
			min-height: 36px;
			padding: 6px 12px 6px 7px;
			border: 1px solid var(--border-soft);
			border-radius: 999px;
		}
		.subnav a.on {
			/* Same weight as idle pills so the row does not shift as you scroll. */
			font-weight: 500;
			border-color: color-mix(in srgb, var(--fill) 60%, transparent);
		}
		.subnav a:focus-visible {
			outline-offset: -2px;
		}
		.subnav .tile {
			width: 22px;
			height: 22px;
			border-radius: 50%;
		}
		/* 40px under the top bar and the pill row. */
		.settings :global(section.set) {
			scroll-margin-top: 140px;
		}
		.settings :global(.row) {
			flex-wrap: wrap;
			padding: 12px 14px;
		}
		.settings :global(.two) {
			grid-template-columns: 1fr;
		}
		/* Touch: controls reach about 36 to 40px. */
		.settings :global(.btn) {
			padding: 8px 14px;
		}
		.settings :global(.seg button) {
			padding: 7px 14px;
		}
		.settings :global(.sel) {
			height: 38px;
		}
		.settings :global(.inp) {
			height: 40px;
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
