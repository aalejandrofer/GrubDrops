<script lang="ts">
	import { onDestroy, onMount, untrack } from 'svelte';
	import { api } from '$lib/app';
	import type { Health, HealthEvent, Platform, Settings, SettingsPatch } from '$lib/api/types';
	import { clock, relTime, uptime } from '$lib/format';
	import { runNowState, type CanaryStamps } from '$lib/canary';
	import { countFilters, eventText, kindChip, matchesFilter, type Filter } from '$lib/health';
	import Icon from '$lib/components/Icon.svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { t, tn } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';
	import { prefs } from '$lib/prefs.svelte';
	import { CANARY_INTERVALS, isChannel, minutesLabel } from '$lib/settings';
	import { SECTION_LOOK, failingCanaries } from '$lib/settings-look';
	import SectionHead from './SectionHead.svelte';

	let {
		s,
		save,
		errs = {},
		failing = $bindable([])
	}: {
		/** Settings for the Advanced block; it hides until they load. */
		s?: Settings;
		save?: (key: string, p: SettingsPatch) => Promise<boolean>;
		errs?: Record<string, boolean>;
		/** Platforms whose canary is failing, for the page's nav dot. */
		failing?: Platform[];
	} = $props();

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
	/** The newest MAX_EVENTS lines; the chips count them and the list filters them. */
	let win = $state<HealthEvent[]>([]);
	const events = $derived(win.filter((e) => matchesFilter(filter, e)));
	const counts = $derived(countFilters(win));
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

	async function load(): Promise<void> {
		const mine = ++seq;
		try {
			const body = await api.health('all');
			if (mine !== seq) return;
			h = body;
			win = body.events;
			loadFailed = false;
			live.seedLog(body.events);
			lastHead = live.log[0];
		} catch {
			if (mine === seq) loadFailed = true;
		}
	}

	function pick(f: Filter): void {
		if (f === filter) return;
		filter = f;
		expanded = false;
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
			await load();
			if (destroyed) return;
			const state = runNowState(before, h?.canary ?? [], Date.now() - clickedAt);
			if (state === 'running') return poll(before, clickedAt);
			canaryNoResult = state === 'no_result' && !loadFailed;
			canaryBusy = false;
		}, CANARY_REFETCH_MS);
	}

	onMount(() => {
		void load();
		clockTimer = setInterval(() => (now = Date.now()), 15000);
	});
	onDestroy(() => {
		destroyed = true;
		clearTimeout(refetchTimer);
		clearInterval(clockTimer);
	});

	/** A new live event joins the window; the counts and the list follow it. */
	$effect(() => {
		const head = live.log[0];
		untrack(() => {
			if (!head || head === lastHead) return;
			lastHead = head;
			if (!h || head.level === 'DEBUG') return;
			win = [head, ...win].slice(0, MAX_EVENTS);
		});
	});

	const canaryFailing = $derived(failingCanaries(h?.canary));
	$effect(() => {
		failing = canaryFailing;
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

	/** Saved canary channel per platform; undefined until settings load. */
	const channelOf = (p: Platform): string | undefined => (s ? (p === 'twitch' ? s.canary.twitch_channel : s.canary.kick_channel).trim() : undefined);
	/** Run now needs at least one saved channel. */
	const noChannel = $derived(!!s && !channelOf('twitch') && !channelOf('kick'));
	/** Platform of the account a log line is about, for the name colour. */
	const platformOf = (e: HealthEvent): Platform | undefined => live.accounts.find((a) => a.id === e.account_id)?.platform;
</script>

<section class="set hue-{SECTION_LOOK.health.hue}" id="health" aria-labelledby="h-hea">
	<SectionHead sec="health" id="h-hea" blurb={t('health.sub')}>
		{#snippet chips()}
			{#each canaryFailing as p, i (i)}<span class="chip bad">{t('health.chip_canary_failing', { platform: t(`accounts.${p}`) })}</span>{/each}
		{/snippet}
	</SectionHead>
	{#if loadFailed}<p class="bad" role="alert">{t('health.load_failed')}</p>{/if}
	{#if h}
		<div class="stats">
			<div class="stat" class:ok={!!h.miner.started_at}><span>{t('health.miner')}</span><b>{minerLine}</b></div>
			<div class="stat"><span>{t('health.sidecar')}</span><b>{tn('health.sidecars', sidecars)}</b></div>
			<div class="stat"><span>{t('health.last_scan')}</span><b>{relTime(h.last_scan_at, now)}</b></div>
		</div>

		<div class="panel canary" id="set-canary">
			<div class="row">
				<div class="lbl">
					<b>{t('health.canary')}</b>
					<small>{t('health.canary_help')}</small>
					{#if noChannel}<small>{t('health.canary_need_channel')}</small>{/if}
					{#if canaryFailed}<small class="bad" role="alert">{t('health.run_failed')}</small>{/if}
					{#if canaryNoResult}<small role="status">{t('health.canary_no_result')}</small>{/if}
				</div>
				{#if noChannel}
					<span class="chip off">{t('settings.unavailable')}</span>
				{:else}
					<button type="button" class="btn tint" disabled={canaryBusy} onclick={runCanary}
						><Icon name="refresh" size={13} />{canaryBusy ? t('health.canary_running') : t('health.run_now')}</button
					>
				{/if}
			</div>
			{#each PLATFORMS as p (p.id)}
				{@const c = canaryOf(p.id)}
				{@const ch = channelOf(p.id)}
				<div class="row can">
					<PlatformMark platform={p.id} />
					<div class="lbl">
						<b class:ch={!!ch}>{ch || t(p.key)}</b>
						{#if ch === ''}
							<small>{t('health.canary_no_channel')}</small>
						{:else if c?.skipped}
							<small>{t('health.canary_skipped', { platform: t(p.key) })}</small>
						{:else if c && !c.ok && c.detail}
							<small class="bad">{c.detail}</small>
						{/if}
					</div>
					{#if ch === ''}
						<span class="chip off">{t('settings.canary_off')}</span>
					{:else if !c || (c.ok && !c.checked_at)}
						<span class="chip off">{t('health.canary_none')}</span>
					{:else if c.skipped}
						<span class="chip off">{t('health.canary_skip')}</span>
					{:else if c.ok}
						<span class="chip ok">{t('health.canary_ok', { rel: relTime(c.checked_at, now) })}</span>
					{:else}
						<span class="chip bad">{t('health.canary_fail')}</span>
					{/if}
				</div>
			{/each}
		</div>

		<div class="panel" id="set-log">
			<div class="row filters">
				<div class="lbl"><b>{t('health.activity')}</b></div>
				<div class="seg" role="group" aria-label={t('health.filter_label')}>
					{#each FILTERS as f (f.id)}
						<button type="button" aria-pressed={filter === f.id} onclick={() => pick(f.id)}>{t(f.key)}<span class="n">{counts[f.id]}</span></button>
					{/each}
				</div>
			</div>
			{#if events.length === 0}
				<p class="empty">{t('health.empty')}</p>
			{:else}
				<ol class="log">
					{#each shownEvents as e, i (`${e.at}-${i}`)}
						{@const chip = kindChip(e)}
						{@const pf = platformOf(e)}
						<li class="ev">
							<time>{e.at ? clock(e.at, prefs.zone, prefs.fmt) : ''}</time>
							<span class="chip plain {chip.tone}">{chip.label}</span>
							<span class="msg"
								>{#if e.account_name}<span class="who pf-name {pf ?? ''}">{e.account_name}</span>{:else}<span class="who sys"
										>{t('health.system')}</span
									>{/if}
								{eventText(e)}</span
							>
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
	{#if s && save}
		<details class="adv">
			<summary>{t('settings.advanced')}</summary>
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
	/* Status strip: three tiles, a green value when the miner runs. */
	.stats {
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		gap: 10px;
		margin-bottom: 12px;
	}
	.stat {
		padding: 10px 14px;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		display: grid;
		gap: 2px;
		min-width: 0;
	}
	.stat span {
		font-size: 12px;
		color: var(--t2);
	}
	.stat b {
		font-weight: 600;
		font-size: 14px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.stat.ok b {
		color: var(--green);
	}
	.canary {
		margin-bottom: 12px;
	}
	.lbl small + small {
		margin-top: 2px;
	}
	.can {
		padding-top: 9px;
		padding-bottom: 9px;
	}
	.can b.ch {
		font-family: var(--mono);
		font-size: 12.5px;
	}
	.filters {
		flex-wrap: wrap;
		gap: 8px 14px;
	}
	.seg .n {
		margin-left: 6px;
		color: var(--t3);
		font-size: 12px;
		font-variant-numeric: tabular-nums;
	}
	.seg button[aria-pressed='true'] .n {
		color: var(--t2);
	}
	/* Activity rows: time, kind chip, then who and what. */
	.log {
		list-style: none;
		margin: 0;
		padding: 0;
		border-top: 1px solid var(--border-soft);
	}
	.ev {
		display: flex;
		align-items: baseline;
		gap: 10px;
		padding: 8px 16px;
		min-width: 0;
	}
	.ev + .ev {
		border-top: 1px solid var(--border-soft);
	}
	.ev time {
		font: 12px var(--mono);
		color: var(--t3);
		flex: none;
		min-width: 40px;
	}
	/* Kind reads as coloured text, no pill; .log .ev .chip.plain outranks the page's .settings .chip.warn pill. */
	.log .ev .chip.plain {
		flex: none;
		min-width: 74px;
		padding: 0;
		border: 0;
		border-radius: 0;
		background: none;
		font-weight: 600;
	}
	.log .ev .chip.plain::before {
		display: none;
	}
	/* Readable violet text on the accent tint; .ev lifts it over the page's base .chip rule. */
	.ev .chip.violet {
		color: #bc8cff;
	}
	:global(:root[data-theme='light']) .ev .chip.violet {
		color: #6236b8;
	}
	.ev .chip.off {
		color: var(--t2);
	}
	.msg {
		font-size: 13px;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.who {
		font-weight: 500;
		margin-right: 2px;
	}
	.sys {
		color: var(--t3);
		font-weight: 400;
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
		font: inherit;
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
	.more:focus-visible,
	.log-foot a:focus-visible {
		outline: 2px solid var(--hue);
		outline-offset: -2px;
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
			grid-template-columns: 1fr 1fr;
		}
		.stat:first-child {
			grid-column: 1 / -1;
		}
		.ev {
			flex-wrap: wrap;
			row-gap: 4px;
			padding: 8px 14px;
		}
		.msg {
			flex-basis: 100%;
		}
	}
</style>
