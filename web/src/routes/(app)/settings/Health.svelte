<script lang="ts">
	import { onDestroy, onMount, untrack } from 'svelte';
	import { api } from '$lib/app';
	import type { Health, HealthEvent, Platform, Settings, SettingsPatch } from '$lib/api/types';
	import { clock, relTime, uptime } from '$lib/format';
	import { runNowState, type CanaryStamps } from '$lib/canary';
	import { eventText, isProblem, kindLabel } from '$lib/health';
	import { t, tn } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';
	import { prefs } from '$lib/prefs.svelte';
	import { CANARY_INTERVALS, isChannel, minutesLabel } from '$lib/settings';

	let {
		s,
		save,
		errs = {}
	}: {
		/** Settings for the Advanced block; it hides until they load. */
		s?: Settings;
		save?: (key: string, p: SettingsPatch) => Promise<boolean>;
		errs?: Record<string, boolean>;
	} = $props();

	type Filter = 'all' | 'problems' | 'claims';
	const FILTERS: { id: Filter; key: string }[] = [
		{ id: 'all', key: 'health.filter.all' },
		{ id: 'problems', key: 'health.filter.problems' },
		{ id: 'claims', key: 'health.filter.claims' }
	];
	const PLATFORMS: { id: Platform; key: string }[] = [
		{ id: 'twitch', key: 'accounts.twitch' },
		{ id: 'kick', key: 'accounts.kick' }
	];
	const MAX_EVENTS = 50;
	const FOLDED_EVENTS = 10;
	const CANARY_REFETCH_MS = 5000;

	const live = getLive();

	let h = $state<Health | null>(null);
	let filter = $state<Filter>('all');
	let events = $state<HealthEvent[]>([]);
	let counts = $state({ all: 0, problems: 0, claims: 0 });
	let loadFailed = $state(false);
	let expanded = $state(false);
	let canaryBusy = $state(false);
	let canaryFailed = $state(false);
	let canaryNoResult = $state(false);
	let now = $state(Date.now());
	let seq = 0;
	let lastHead: HealthEvent | undefined;
	let destroyed = false;
	let refetchTimer: ReturnType<typeof setTimeout> | undefined;
	let clockTimer: ReturnType<typeof setInterval> | undefined;

	const matches = (f: Filter, e: HealthEvent): boolean =>
		f === 'all' || (f === 'problems' ? isProblem(e) : e.kind === 'claim');

	async function load(f: Filter): Promise<void> {
		const mine = ++seq;
		try {
			const body = await api.health(f);
			if (mine !== seq) return;
			h = body;
			events = body.events;
			counts = body.counts;
			loadFailed = false;
			if (f === 'all') live.seedLog(body.events);
			lastHead = live.log[0];
		} catch {
			if (mine === seq) loadFailed = true;
		}
	}

	function pick(f: Filter): void {
		if (f === filter) return;
		filter = f;
		expanded = false;
		void load(f);
	}

	async function runCanary(): Promise<void> {
		const before = Object.fromEntries((h?.canary ?? []).map((c) => [c.platform, c.checked_at]));
		const clickedAt = Date.now();
		canaryBusy = true;
		canaryFailed = false;
		canaryNoResult = false;
		try {
			await api.runCanary();
		} catch {
			canaryFailed = true;
			canaryBusy = false;
			return;
		}
		poll(before, clickedAt);
	}

	/** Refetch health every 5 s until a canary result changes or the wait runs out. */
	function poll(before: CanaryStamps, clickedAt: number): void {
		clearTimeout(refetchTimer);
		refetchTimer = setTimeout(async () => {
			if (destroyed) return;
			await load(filter);
			if (destroyed) return;
			const state = runNowState(before, h?.canary ?? [], Date.now() - clickedAt);
			if (state === 'running') return poll(before, clickedAt);
			canaryNoResult = state === 'no_result' && !loadFailed;
			canaryBusy = false;
		}, CANARY_REFETCH_MS);
	}

	onMount(() => {
		void load('all');
		clockTimer = setInterval(() => (now = Date.now()), 15000);
	});
	onDestroy(() => {
		destroyed = true;
		clearTimeout(refetchTimer);
		clearInterval(clockTimer);
	});

	/** A new live event bumps the chip counts and joins the list when it matches the chip. */
	$effect(() => {
		const head = live.log[0];
		untrack(() => {
			if (!head || head === lastHead) return;
			lastHead = head;
			if (!h || head.level === 'DEBUG') return;
			counts = {
				all: counts.all + 1,
				problems: counts.problems + (isProblem(head) ? 1 : 0),
				claims: counts.claims + (head.kind === 'claim' ? 1 : 0)
			};
			if (matches(filter, head)) events = [head, ...events].slice(0, MAX_EVENTS);
		});
	});

	const shownEvents = $derived(expanded ? events : events.slice(0, FOLDED_EVENTS));
	const canaryOf = (p: Platform) => h?.canary.find((c) => c.platform === p);
	const sidecars = $derived(h?.kick_sidecars.running.length ?? 0);
	const minerLine = $derived(
		h?.miner.started_at ? t('health.running', { uptime: uptime(h.miner.started_at, now) }).trim() : '–'
	);
	let twitchCh = $state('');
	let kickCh = $state('');
	let chDirty = $state(false);
	let chBad = $state(false);
	$effect(() => {
		if (s && !chDirty) {
			twitchCh = s.canary.twitch_channel;
			kickCh = s.canary.kick_channel;
		}
	});
	const canaryIntervals = $derived(
		!s || CANARY_INTERVALS.includes(s.canary.interval_sec) ? CANARY_INTERVALS : [...CANARY_INTERVALS, s.canary.interval_sec]
	);
	const intervalLabel = (sec: number): string => (sec === 0 ? t('settings.canary_off') : minutesLabel(Math.round(sec / 60)));

	async function saveChannels(): Promise<void> {
		if (!s || !save) return;
		const tw = twitchCh.trim();
		const kc = kickCh.trim();
		if (!isChannel(tw) || !isChannel(kc)) {
			chBad = true;
			return;
		}
		if (await save('canary_channels', { canary: { ...s.canary, twitch_channel: tw, kick_channel: kc } })) chDirty = false;
	}

	function chKey(e: KeyboardEvent): void {
		if (e.key === 'Enter' && e.isTrusted && chDirty) void saveChannels();
	}

	const levelClass = (e: HealthEvent): string =>
		e.level === 'ERROR' ? 'err' : e.level === 'WARN' ? 'warn' : e.kind === 'claim' ? 'ok' : 'info';
</script>

<section class="set" id="health" aria-labelledby="h-hea">
	<div class="sec-head">
		<div>
			<h2 id="h-hea">{t('settings.health')}</h2>
			<p>{t('health.sub')}</p>
		</div>
	</div>
	{#if loadFailed}<p class="bad" role="alert">{t('health.load_failed')}</p>{/if}
	{#if h}
		<div class="panel stats">
			<div><small class="ok">{t('health.miner')}</small><b>{minerLine}</b></div>
			<div><small class={sidecars > 0 ? 'ok' : ''}>{t('health.sidecar')}</small><b>{tn('health.sidecars', sidecars)}</b></div>
			<div><small class={h.last_scan_at ? 'ok' : ''}>{t('health.last_scan')}</small><b>{relTime(h.last_scan_at, now)}</b></div>
		</div>
		<div class="panel">
			<div class="row filters">
				<div class="chips" role="group" aria-label={t('health.filter_label')}>
					{#each FILTERS as f (f.id)}
						<button type="button" class="chip" class:on={filter === f.id} aria-pressed={filter === f.id} onclick={() => pick(f.id)}
							>{t(f.key)}<span class="n">{counts[f.id]}</span></button
						>
					{/each}
				</div>
			</div>
			{#if events.length === 0}
				<p class="empty">{t('health.empty')}</p>
			{:else}
				<ol class="log">
					{#each shownEvents as e, i (`${e.at}-${i}`)}
						<li class={levelClass(e)}>
							<time>{e.at ? clock(e.at, prefs.zone, prefs.fmt) : ''}</time>
							<span class="lv {levelClass(e)}"></span>
							<span class="acc">{e.account_name || t('health.system')}</span>
							<span class="msg">{#if e.kind}<span class="kind">{kindLabel(e.kind)}</span>{/if}{eventText(e)}</span>
						</li>
					{/each}
				</ol>
				{#if events.length > FOLDED_EVENTS}
					<button type="button" class="more" aria-expanded={expanded} onclick={() => (expanded = !expanded)}
						>{expanded ? t('health.show_less') : tn('health.show_all', events.length)}</button
					>
				{/if}
			{/if}
			<div class="log-foot">
				<span>{tn('health.foot', events.length)}</span>
				<a href="/api/health/log" download>{t('health.download')}</a>
			</div>
		</div>
	{/if}
	{#if h || (s && save)}
		<details class="adv">
			<summary>{t('settings.advanced')}</summary>
			{#if h}
				<div class="panel canary">
					<div class="row">
						<div class="lbl">
							<b class="tip-wrap"
								>{t('health.canary')}<button type="button" class="tip" aria-label={t('health.canary')} aria-describedby="canary-tip">?</button><span
									class="tip-text"
									id="canary-tip"
									role="tooltip">{t('health.canary_help')}</span
								></b
							>
							{#if canaryFailed}<small class="bad" role="alert">{t('health.run_failed')}</small>{/if}
							{#if canaryNoResult}<small role="status">{t('health.canary_no_result')}</small>{/if}
						</div>
						<button type="button" class="btn" disabled={canaryBusy} onclick={runCanary}>{canaryBusy ? t('health.canary_running') : t('health.run_now')}</button>
					</div>
					{#each PLATFORMS as p (p.id)}
						{@const c = canaryOf(p.id)}
						<div class="row">
							<div class="lbl">
								<b>{t(p.key)}</b>
								{#if !c || (c.ok && !c.checked_at)}
									<small>{t('health.canary_none')}</small>
								{:else if c.skipped}
									<small>{t('health.canary_skipped', { platform: t(p.key) })}</small>
								{:else if c.ok}
									<small class="good">{t('health.canary_ok', { rel: relTime(c.checked_at, now) })}</small>
								{:else}
									<small class="bad">{t('health.canary_fail', { detail: c.detail })}</small>
								{/if}
							</div>
						</div>
					{/each}
				</div>
			{/if}
			{#if s && save}
				<div class="panel">
					<div class="row">
						<div class="lbl">
							<b><label for="canary-int">{t('settings.canary_interval')}</label></b>
							<small>{t('settings.canary_interval_sub')} {t('settings.after_restart')}</small>
							{#if errs.canary_interval}<small class="bad">{t('settings.save_failed')}</small>{/if}
						</div>
						<select
							class="sel"
							id="canary-int"
							autocomplete="off"
							value={s.canary.interval_sec}
							onchange={(e) => {
								const v = Number(e.currentTarget.value);
								if (e.isTrusted && s && v !== s.canary.interval_sec) void save('canary_interval', { canary: { ...s.canary, interval_sec: v } });
							}}
						>
							{#each canaryIntervals as sec (sec)}<option value={sec}>{intervalLabel(sec)}</option>{/each}
						</select>
					</div>
					<div class="card-body">
						<div class="two">
							<div class="field">
								<label for="canary-tw">{t('settings.canary_twitch')}</label>
								<input
									class="inp"
									id="canary-tw"
									type="text"
									autocomplete="off"
									spellcheck="false"
									aria-invalid={chBad ? true : undefined}
									bind:value={twitchCh}
									oninput={() => ((chDirty = true), (chBad = false))}
									onkeydown={chKey}
								/>
							</div>
							<div class="field">
								<label for="canary-kick">{t('settings.canary_kick')}</label>
								<input
									class="inp"
									id="canary-kick"
									type="text"
									autocomplete="off"
									spellcheck="false"
									placeholder={t('settings.canary_kick_ph')}
									aria-invalid={chBad ? true : undefined}
									bind:value={kickCh}
									oninput={() => ((chDirty = true), (chBad = false))}
									onkeydown={chKey}
								/>
							</div>
						</div>
						<div class="inline wrap">
							<button type="button" class="btn" disabled={!chDirty} onclick={saveChannels}>{t('accounts.save')}</button>
						</div>
						<small class="help">{t('settings.canary_channels_help')}</small>
						{#if chBad}<span class="feedback bad" role="alert">{t('settings.canary_channel_invalid')}</span>{/if}
						{#if errs.canary_channels}<span class="feedback bad">{t('settings.save_failed')}</span>{/if}
					</div>
				</div>
			{/if}
		</details>
	{/if}
</section>

<style>
	.bad {
		color: var(--red);
	}
	.help {
		color: var(--t2);
		font-size: 12px;
	}
	.good {
		color: var(--green);
	}
	.stats {
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		margin-bottom: 14px;
	}
	.stats > div {
		padding: 12px 16px;
		display: grid;
		gap: 2px;
		min-width: 0;
	}
	.stats > div + div {
		border-left: 1px solid var(--border-soft);
	}
	.stats small {
		color: var(--t2);
		font-size: 12px;
		display: flex;
		align-items: center;
		gap: 6px;
	}
	.stats b {
		font-size: 14px;
		font-weight: 600;
	}
	.stats .ok::before {
		content: '';
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: var(--green);
	}
	.canary {
		margin-bottom: 14px;
	}
	.tip-wrap {
		position: relative;
		display: inline-flex;
		align-items: center;
		gap: 6px;
	}
	.tip {
		display: inline-grid;
		place-items: center;
		width: 16px;
		height: 16px;
		padding: 0;
		border: 1px solid var(--border);
		border-radius: 50%;
		background: none;
		color: var(--t2);
		font: 600 10.5px/1 inherit;
		cursor: help;
		margin-left: 6px;
	}
	.tip:hover,
	.tip:focus-visible {
		color: var(--text);
		outline: none;
		border-color: var(--t2);
	}
	.tip-text {
		position: absolute;
		top: calc(100% + 6px);
		left: 0;
		z-index: 5;
		width: max-content;
		max-width: min(300px, 70vw);
		padding: 8px 10px;
		border: 1px solid var(--border);
		border-radius: 8px;
		background: var(--surface-3);
		color: var(--text);
		font-size: 12px;
		font-weight: 400;
		line-height: 1.45;
		visibility: hidden;
		opacity: 0;
		transition: opacity 0.12s;
	}
	.tip-wrap:hover .tip-text,
	.tip:focus-visible + .tip-text {
		visibility: visible;
		opacity: 1;
	}
	.filters {
		padding: 10px 16px;
	}
	.chips {
		display: flex;
		gap: 6px;
		flex-wrap: wrap;
	}
	.chip {
		font: inherit;
		font-size: 12.5px;
		font-weight: 500;
		padding: 3px 10px;
		border-radius: 12px;
		border: 1px solid var(--border);
		background: none;
		color: var(--t2);
		cursor: pointer;
	}
	.chip:hover {
		color: var(--text);
	}
	.chip.on {
		color: var(--text);
		background: var(--surface-3);
		border-color: var(--surface-3);
	}
	.chip:focus-visible,
	.log-foot a:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.chip .n {
		color: var(--t3);
		margin-left: 4px;
		font-family: var(--mono);
		font-size: 12px;
	}
	.log {
		list-style: none;
		margin: 0;
		padding: 6px 0;
		border-top: 1px solid var(--border-soft);
		font: 12.5px/1.5 var(--mono);
	}
	.log li {
		display: grid;
		grid-template-columns: 56px 8px 104px minmax(0, 1fr);
		gap: 10px;
		align-items: baseline;
		padding: 6px 16px;
	}
	.log li + li {
		border-top: 1px solid var(--border-soft);
	}
	.log li:hover {
		background: var(--surface-2);
	}
	.log time {
		color: var(--t3);
		white-space: nowrap;
	}
	.lv {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		/* Centre on the first line, so a wrapped message keeps its dot at the top. */
		align-self: start;
		margin-top: calc(0.75em - 3.5px);
	}
	.lv.info {
		background: var(--t3);
	}
	.lv.ok {
		background: var(--green);
	}
	.lv.warn {
		background: var(--amber);
	}
	.lv.err {
		background: var(--red);
	}
	.acc {
		color: var(--t2);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.msg {
		color: var(--text);
		overflow-wrap: anywhere;
	}
	.kind {
		margin-right: 6px;
		font-weight: 600;
	}
	.log li.ok .msg {
		color: var(--green);
	}
	.log li.warn .msg {
		color: var(--amber);
	}
	.log li.err .msg {
		color: var(--red);
	}
	.empty {
		margin: 0;
		padding: 14px 16px;
		border-top: 1px solid var(--border-soft);
		color: var(--t2);
		font-size: 13px;
	}
	.more {
		display: block;
		width: 100%;
		padding: 8px 16px;
		font-size: 12px;
		color: var(--t2);
		text-align: left;
		background: none;
		border: 0;
		border-top: 1px solid var(--border-soft);
		cursor: pointer;
	}
	.more:hover {
		color: var(--text);
		background: var(--surface-2);
	}
	.log-foot {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 8px;
		padding: 8px 16px;
		border-top: 1px solid var(--border-soft);
		color: var(--t3);
		font-size: 12px;
	}
	.log-foot a {
		color: var(--t2);
	}
	.log-foot a:hover {
		color: var(--text);
	}
	@media (max-width: 720px) {
		.stats {
			grid-template-columns: 1fr;
		}
		.stats > div + div {
			border-left: 0;
			border-top: 1px solid var(--border-soft);
		}
		.log li {
			grid-template-columns: 52px 8px minmax(0, 1fr);
			gap: 8px;
			padding: 6px 14px;
		}
		.acc {
			grid-column: 3;
		}
		.msg {
			grid-column: 3;
			margin-top: -2px;
		}
	}
</style>
