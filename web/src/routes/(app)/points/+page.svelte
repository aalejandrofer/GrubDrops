<script lang="ts">
	import { onDestroy } from 'svelte';
	import { api } from '$lib/app';
	import type { Account, ForceChannel, Platform, PointsMode } from '$lib/api/types';
	import Icon from '$lib/components/Icon.svelte';
	import PageHead from '$lib/components/PageHead.svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { accountStatus, forceWatchOn, pointsSupported } from '$lib/accounts';
	import { reorder } from '$lib/drag';
	import { toPut, watchingIds } from '$lib/force-watch';
	import { formatInt, msToNextMinute, relTime } from '$lib/format';
	import { t } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';
	import {
		BAL_KEYS,
		PLATFORMS,
		accountFresh,
		balanceIndex,
		balanceOf,
		balanceView,
		cardState,
		chanKey,
		freshLine,
		freshTone,
		fwNote,
		moveInTab,
		pointsTickMs,
		pointsTotal,
		removeChannel,
		tabRows,
		toggleAccount,
		watchers,
		watchStatus,
		type BalView,
		type Fresh
	} from '$lib/points';
	import { moveItem } from '$lib/upnext';
	import { refreshLabel } from '$lib/cooldown';
	import { createRefresh } from '$lib/refresh.svelte';
	import AddChannel from './AddChannel.svelte';
	import PointsModePicker from '$lib/components/PointsMode.svelte';

	/**
	 * Home of the Force watch list: one global ordered list shown per
	 * platform, account toggles per channel, each account's points mode, and
	 * cached channel-points balances. Every list change saves the whole list.
	 */
	const live = getLive();
	const LABEL: Record<Platform, string> = { twitch: 'Twitch', kick: 'Kick' };

	let tab = $state<Platform>('twitch');
	let now = $state(Date.now());
	// Ages tick every 5s while a balance is under a minute old, then every 30s.
	// New balances restart the clock.
	$effect(() => {
		const p = live.points;
		let timer: ReturnType<typeof setTimeout>;
		const tick = (): void => {
			now = Date.now();
			timer = setTimeout(tick, pointsTickMs(p, now));
		};
		timer = setTimeout(tick, pointsTickMs(p, Date.now()));
		return () => clearTimeout(timer);
	});
	// Watch times on the account cards turn over on the minute.
	let minuteNow = $state(Date.now());
	let minuteTimer: ReturnType<typeof setTimeout> | undefined;
	function minuteTick(): void {
		minuteNow = Date.now();
		minuteTimer = setTimeout(minuteTick, msToNextMinute(minuteNow));
	}
	minuteTimer = setTimeout(minuteTick, msToNextMinute(Date.now()));

	const list = $derived(live.forceWatch ?? []);
	const rows = $derived(tabRows(list, tab));
	const accts = $derived(live.accounts.filter((a) => a.platform === tab));
	const hasPoints = $derived(pointsSupported({ platform: tab }));
	const idx = $derived(balanceIndex(live.points));
	const names = $derived(new Map(live.accounts.map((a) => [a.id, a.name])));
	const known = $derived(new Set(live.accounts.map((a) => a.id)));
	const who = $derived(watchers(list, live.runners, names));
	const runnerOf = (id: string) => live.runners.find((r) => r.account_id === id);

	const status = $derived(watchStatus(who));
	const stamp = $derived(
		hasPoints && live.points ? t('points.refresh_every', { n: Math.round(live.points.refresh_every_sec / 60) }) : undefined
	);

	/** Row being dragged, previewed at its drop slot. */
	let drag = $state<{ from: number; to: number } | null>(null);
	const shown = $derived(drag ? moveItem(rows, drag.from, drag.to) : rows);
	const dragKey = $derived(drag ? chanKey(rows[drag.from]) : null);
	let moved = $state<string | null>(null);
	let arming = $state<string | null>(null);
	let failed = $state(false);
	let modeErr = $state<Record<string, string>>({});

	let flashTimer: ReturnType<typeof setTimeout> | undefined;
	function flash(key: string): void {
		moved = key;
		clearTimeout(flashTimer);
		flashTimer = setTimeout(() => (moved = null), 900);
	}
	// Refresh now: busy until the next points push lands, then green for a moment.
	const rf = createRefresh({ post: () => api.refreshPoints(), signal: () => live.points });
	const refreshText = $derived(t(refreshLabel(rf.busy, rf.done, 'points.refresh_now')));

	onDestroy(() => {
		clearTimeout(flashTimer);
		clearTimeout(minuteTimer);
	});

	// Saves run one at a time, each with the whole list as it was then. Only
	// the last one's answer replaces the list, so quick edits don't flicker.
	let pending = 0;
	let chain: Promise<unknown> = Promise.resolve();
	async function commit(next: ForceChannel[]): Promise<void> {
		// Saving before the list loads would replace it with this partial one.
		if (live.forceWatch === null) return;
		live.forceWatch = next;
		failed = false;
		arming = null;
		pending++;
		const body = toPut(next, known);
		const run = chain.then(() => api.putForceWatch(body));
		chain = run.catch(() => {});
		try {
			const r = await run;
			if (pending === 1) {
				live.forceWatch = r.channels;
				failed = false;
			}
		} catch {
			failed = true;
			if (pending === 1) {
				try {
					live.forceWatch = (await api.forceWatch()).channels;
				} catch {
					/* the force_watch event refetches */
				}
			}
		} finally {
			pending--;
		}
	}

	function move(from: number, to: number): void {
		if (to < 0 || to >= rows.length || from === to) return;
		flash(chanKey(rows[from]));
		void commit(moveInTab(list, tab, from, to));
	}

	function remove(c: ForceChannel): void {
		const k = chanKey(c);
		if (arming !== k) {
			arming = k;
			return;
		}
		void commit(removeChannel(list, k));
	}

	function add(next: ForceChannel[], key: string): void {
		flash(key);
		void commit(next);
	}

	async function setMode(a: Account, mode: PointsMode): Promise<void> {
		const prev = a.points_mode;
		modeErr[a.id] = '';
		a.points_mode = mode;
		try {
			await api.setPointsMode(a.id, mode);
		} catch {
			a.points_mode = prev;
			modeErr[a.id] = t('accounts.points_failed', { name: a.name });
		}
	}

	const dragOpts = {
		enabled: () => rows.length > 1,
		onStart: (from: number) => (drag = { from, to: from }),
		onMove: (to: number) => {
			if (drag) drag = { ...drag, to };
		},
		onEnd: (from: number, to: number) => {
			drag = null;
			move(from, to);
		},
		onCancel: () => (drag = null)
	};

	const freshText = (f: Fresh) => freshLine(f, now);

	function chipTitle(a: Account, bv: BalView | null): string {
		const note = `${a.name}: ${t(fwNote(a, runnerOf(a.id)))}`;
		return bv ? `${note}. ${t(BAL_KEYS[bv.tone], { time: relTime(bv.at, now) })}` : note;
	}
	const balText = (bv: BalView) => (bv.value !== null ? formatInt(bv.value) : bv.tone === 'pending' ? '…' : '–');
</script>

{#snippet refresh()}
	<span class="refresh">
		<span class="stamp">{stamp}</span>
		{#if rf.failed}<small class="bad" role="alert">{t('points.refresh_failed')}</small>{/if}
		<button
			type="button"
			class="btn rf"
			class:spin={rf.busy}
			class:done={rf.done}
			disabled={rf.busy}
			onclick={() => rf.run()}><Icon name="refresh" />{refreshText}</button
		>
	</span>
{/snippet}

<PageHead
	title={t('nav.points')}
	{status}
	lead={who.length ? undefined : t('points.nobody')}
	{stamp}
	action={stamp ? refresh : undefined}
>
	<div class="tabs" role="tablist" aria-label={t('discover.filter_platform')}>
		{#each PLATFORMS as p (p)}
			<button
				type="button"
				role="tab"
				aria-selected={tab === p}
				onclick={() => {
					tab = p;
					drag = null;
					arming = null;
				}}
			>
				<PlatformMark platform={p} />{LABEL[p]}<span class="n">{tabRows(list, p).length}</span>
			</button>
		{/each}
	</div>

	{#if accts.length}
		<h2 class="rh" id="points-accounts">{t('points.accounts')}</h2>
		<section class="strip" aria-labelledby="points-accounts">
			{#each accts as a (a.id)}
				{@const f = accountFresh(live.points, a.id)}
				{@const st = cardState(a, runnerOf(a.id), minuteNow)}
				{@const sum = pointsTotal(live.points, a.id)}
				<div class="acc" class:paused={!a.enabled} class:on={st.on}>
					<div class="top">
						<b class="pf-name {a.platform}">{a.name}</b>
						{#if sum !== null}<span class="sum" class:stale={freshTone(f) !== ''}
								>{formatInt(sum)}<small>{t('points.pts')}</small></span
							>{/if}
					</div>
					<div class="state" class:on={st.on}>
						<span class="sd" aria-hidden="true"></span>
						<span class="txt" title={st.text}
							>{#each st.parts as p, i (i)}{#if p.kind === 'channel'}<b>{p.text}</b>{:else if p.kind === 'time'}<span
										class="tm">{p.text}</span
									>{:else}{p.text}{/if}{/each}</span
						>
						{#if !a.enabled && accountStatus(a, runnerOf(a.id)) !== 'paused'}<span class="tag"
								>{t('accounts.status.paused')}</span
							>{/if}
					</div>
					{#if pointsSupported(a)}
						<PointsModePicker {a} onpick={(m) => setMode(a, m)} />
						{#if freshText(f)}<div class="tot"><small class="fresh-{freshTone(f)}">{freshText(f)}</small></div>{/if}
						{#if modeErr[a.id]}<small class="bad" role="alert">{modeErr[a.id]}</small>{/if}
					{:else}
						<div class="tot"><small>{t('points.unsupported', { platform: LABEL[a.platform] })}</small></div>
					{/if}
				</div>
			{/each}
		</section>
	{/if}

	<h2 class="rh">{t('points.order')}</h2>
	{#if failed}<p class="bad" role="alert">{t('force.save_failed')}</p>{/if}
	<ol use:reorder={dragOpts}>
		{#each shown as c, i (chanKey(c))}
			{@const k = chanKey(c)}
			{@const nowIds = c.live ? watchingIds(c, live.runners) : []}
			<li class:moved={moved === k} class:dragging={dragKey === k}>
				<div class="grip">
					<button
						type="button"
						aria-label={t('force.move_up')}
						title={t('force.move_up')}
						disabled={i === 0}
						onclick={() => move(i, i - 1)}
					>
						<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 15 6-6 6 6" /></svg>
					</button>
					<span class="h" role="presentation">
						<svg class="i" viewBox="0 0 24 24" aria-hidden="true"
							><path d="M9 6h.01M15 6h.01M9 12h.01M15 12h.01M9 18h.01M15 18h.01" stroke-width="3" /></svg
						>
					</span>
					<button
						type="button"
						aria-label={t('force.move_down')}
						title={t('force.move_down')}
						disabled={i === shown.length - 1}
						onclick={() => move(i, i + 1)}
					>
						<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg>
					</button>
				</div>
				<div class="mid">
					<span class="ch">
						<span class="rk">{String(i + 1).padStart(2, '0')}</span>
						<PlatformMark platform={c.platform} />
						<span class="who">
							<b
								><span class="nm">{c.channel}</span>{#if c.live !== null}<span
										class="dot"
										class:live={c.live}
										role="img"
										aria-label={c.live ? t('force.live') : t('force.offline')}
										title={c.live ? t('force.live') : t('force.offline')}
									></span>{/if}</b
							>
							<small>
								{#if nowIds.length}<span class="on"
										>{t('points.chan_watching', { names: nowIds.map((id) => names.get(id) ?? id).join(', ') })}</span
									>
								{:else if c.live}{t('points.chan_live')}
								{:else if c.live === false}{t('force.offline')}
								{:else}{t('points.chan_unchecked')}{/if}
							</small>
						</span>
					</span>
					<div class="chips" role="group" aria-label={t('points.chips', { channel: c.channel })}>
						{#each accts as a (a.id)}
							{@const on = c.accounts.includes(a.id)}
							{@const bv = on && pointsSupported(a) ? balanceView(balanceOf(idx, c, a.id)) : null}
							<button
								type="button"
								class="chip"
								class:on
								class:paused={!a.enabled}
								class:skip={on && (!a.enabled || !forceWatchOn(a))}
								aria-pressed={on}
								title={chipTitle(a, bv)}
								onclick={() => commit(toggleAccount(list, k, a.id))}
							>
								{#if nowIds.includes(a.id)}<span class="live" role="img" aria-label={t('points.watching_now')}></span>{/if}
								<span class="nm">{a.name}</span>
								{#if bv}<span class="bal mono {bv.tone}">{balText(bv)}</span>{/if}
							</button>
						{/each}
					</div>
				</div>
				<button
					type="button"
					class="rmv"
					class:armed={arming === k}
					aria-label={arming === k ? t('force.remove') : `${t('force.remove')} ${c.channel}`}
					title={t('force.remove')}
					onclick={() => remove(c)}
					onblur={() => arming === k && (arming = null)}
					>{#if arming === k}{t('force.remove')}{:else}<Icon name="x" />{/if}</button
				>
			</li>
		{:else}
			<li class="empty">{t('force.empty')}</li>
		{/each}
	</ol>
	{#if hasPoints && rows.length}<p class="legend">{t('points.legend')}</p>{/if}

	{#if live.forceWatch !== null}<AddChannel {tab} {list} label={LABEL} onadd={add} />{/if}
</PageHead>

<style>
	.refresh {
		display: inline-flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 10px;
	}
	.refresh .stamp {
		color: var(--t3);
		font-size: 12px;
		font-family: var(--mono);
	}
	.refresh .bad {
		margin: 0;
	}
	.btn {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font-size: 12px;
		font-weight: 500;
		background: var(--surface-2);
		border: 1px solid var(--border);
		border-radius: 6px;
		padding: 3px 9px;
		color: var(--text);
		white-space: nowrap;
		cursor: pointer;
	}
	.btn:hover:not([disabled]) {
		border-color: var(--t3);
		background: var(--surface-3);
	}
	.btn[disabled] {
		opacity: 0.45;
		cursor: default;
	}
	.btn:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.tabs {
		display: flex;
		gap: 4px;
		border-bottom: 1px solid var(--border-soft);
		margin-bottom: 20px;
	}
	.tabs button {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		font: inherit;
		font-size: 13.5px;
		font-weight: 500;
		padding: 8px 12px;
		margin-bottom: -1px;
		border: 0;
		border-bottom: 2px solid transparent;
		background: none;
		color: var(--t2);
		cursor: pointer;
	}
	.tabs button:hover,
	.tabs button[aria-selected='true'] {
		color: var(--text);
	}
	.tabs button[aria-selected='true'] {
		border-bottom-color: var(--accent);
	}
	.n {
		font: 500 11px/1 var(--mono);
		padding: 3px 6px;
		border-radius: 999px;
		background: var(--surface-2);
		color: var(--t3);
	}
	.strip {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(min(300px, 100%), 1fr));
		gap: 10px;
		margin-bottom: 20px;
	}
	/* Account card: what the account is doing, what it has earned, its mode. */
	.acc {
		display: grid;
		gap: 10px;
		padding: 12px 14px;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		align-content: start;
		min-width: 0;
	}
	.acc.on {
		background: color-mix(in srgb, var(--green) 6%, var(--surface));
		border-color: color-mix(in srgb, var(--green) 22%, var(--border-soft));
	}
	.top {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: 8px;
		min-width: 0;
	}
	.top b {
		font-size: 14px;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.sum {
		flex: none;
		font-size: 17px;
		font-weight: 600;
		letter-spacing: -0.02em;
		font-variant-numeric: tabular-nums;
		color: var(--text);
	}
	.sum small {
		margin-left: 3px;
		font-size: 12px;
		font-weight: 500;
		letter-spacing: 0;
		color: var(--t2);
	}
	.sum.stale {
		color: var(--t2);
	}
	.state {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 13px;
		color: var(--t2);
		min-width: 0;
	}
	.state .txt {
		min-width: 0;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.state .sd {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--t3);
		flex: none;
	}
	.state.on {
		color: var(--text);
	}
	.state.on .sd {
		background: var(--green);
		box-shadow: 0 0 0 3px var(--green-soft);
	}
	.state b {
		font-weight: 600;
	}
	.state .tm {
		color: var(--green);
		font-weight: 600;
		font-variant-numeric: tabular-nums;
		white-space: nowrap;
	}
	.tot {
		display: flex;
		align-items: baseline;
		flex-wrap: wrap;
		gap: 4px 8px;
		font-size: 12px;
		color: var(--t3);
	}
	.tot small {
		font-size: 11.5px;
	}
	.mono {
		font-family: var(--mono);
		font-variant-numeric: tabular-nums;
	}
	.paused {
		opacity: 0.55;
	}
	.tag {
		flex: none;
		font-size: 10.5px;
		font-weight: 600;
		padding: 1px 6px;
		border-radius: 999px;
		background: var(--surface-3);
		color: var(--t2);
		white-space: nowrap;
	}
	.fresh-stale {
		color: var(--amber);
	}
	.fresh-failed {
		color: var(--red);
	}
	.bad {
		margin: 0 0 8px;
		font-size: 12px;
		color: var(--red);
	}
	.acc .bad {
		margin: 0;
	}
	.rh {
		margin: 0 0 8px;
		font-size: 11px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--t3);
	}
	ol {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 6px;
	}
	li {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 8px 8px 8px 4px;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		transition: border-color 0.15s;
		min-width: 0;
	}
	li.empty {
		padding: 14px;
		color: var(--dim);
		font-size: 13px;
	}
	li.moved {
		border-color: var(--accent);
	}
	li.dragging {
		border-color: var(--accent);
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
	}
	.grip .h {
		cursor: grab;
		padding: 2px 0;
		touch-action: none;
		display: grid;
	}
	.grip button {
		width: 26px;
		height: 22px;
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
	.grip button:disabled {
		opacity: 0.3;
		cursor: default;
	}
	.i {
		width: 14px;
		height: 14px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.8;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
	.mid {
		flex: 1;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: 8px 16px;
		flex-wrap: wrap;
	}
	.ch {
		display: flex;
		align-items: center;
		gap: 10px;
		min-width: 0;
		flex: 0 1 220px;
	}
	.rk {
		font: 500 12px/1 var(--mono);
		color: var(--t3);
		flex: none;
	}
	.who {
		display: grid;
		min-width: 0;
	}
	.who b {
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: 14px;
		font-weight: 600;
		min-width: 0;
	}
	.nm {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--surface-3);
		flex: none;
	}
	.dot.live {
		background: var(--red);
	}
	.who small {
		font-size: 12px;
		color: var(--t3);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.who .on {
		color: var(--green);
		font-weight: 600;
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		flex: 1;
		min-width: 0;
	}
	.chip {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font: inherit;
		font-size: 12px;
		height: 26px;
		max-width: 100%;
		padding: 0 10px;
		border-radius: 999px;
		border: 1px solid var(--border);
		background: var(--surface-2);
		color: var(--t3);
		cursor: pointer;
	}
	.chip.on {
		border-color: var(--accent);
		background: var(--accent-soft);
		color: var(--text);
	}
	.chip.skip {
		border-style: dashed;
	}
	.chip .bal {
		font-size: 11.5px;
		color: var(--t2);
	}
	.chip .bal.stale,
	.chip .bal.off {
		color: var(--amber);
	}
	.chip .bal.error {
		color: var(--red);
	}
	.chip .bal.pending,
	.chip .bal.na {
		color: var(--dim);
	}
	.live {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--green);
		box-shadow: 0 0 0 3px var(--green-soft);
		flex: none;
	}
	.rmv {
		min-width: 26px;
		height: 26px;
		display: grid;
		place-items: center;
		flex: none;
		border: 0;
		border-radius: 6px;
		background: none;
		color: var(--t3);
		font: inherit;
		font-size: 12px;
		cursor: pointer;
	}
	.rmv:hover,
	.rmv.armed {
		background: var(--red-soft);
		color: var(--red);
	}
	.rmv.armed {
		padding: 0 8px;
		font-weight: 600;
	}
	.legend {
		margin: 8px 0 0;
		font-size: 12px;
		color: var(--dim);
	}
	.tabs button:focus-visible,
	.grip button:focus-visible,
	.chip:focus-visible,
	.rmv:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	@media (max-width: 640px) {
		.ch {
			flex-basis: 100%;
		}
	}
</style>
