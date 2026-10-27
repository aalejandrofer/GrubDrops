<script lang="ts">
	import { api } from '$lib/app';
	import { etaShort, nextScanMin } from '$lib/format';
	import { t, tn } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';
	import { prefs } from '$lib/prefs.svelte';
	import { reorder } from '$lib/drag';
	import { afterLabel, anyCustom, commitMineNow, commitMove, foldQueue, moveItem, queues } from '$lib/upnext';
	import type { Runner } from '$lib/api/types';
	import type { CampaignTarget } from './CampaignDrawer.svelte';
	import PlatformMark from './PlatformMark.svelte';

	/** Opens the campaign drawer; without it the campaign name is plain text. */
	let { onopen }: { onopen?: (t: CampaignTarget) => void } = $props();

	const live = getLive();
	let failed = $state(false);
	// Per-account Mine next state.
	let mineBusy = $state<Record<string, boolean>>({});
	let mineFailed = $state<Record<string, boolean>>({});
	let drag = $state<{ id: string; from: number; to: number } | null>(null);
	// Accounts whose queue shows every row; not persisted.
	let open = $state<Record<string, boolean>>({});
	let now = $state(Date.now());
	$effect(() => {
		const id = setInterval(() => (now = Date.now()), 30000);
		return () => clearInterval(id);
	});

	const qs = $derived(queues(live.runners));
	const total = $derived(qs.reduce((n, q) => n + q.items.length, 0));
	const custom = $derived(anyCustom(live.runners));
	// The overview carries the last scan; fall back to the latest per-runner scan.
	const lastScan = $derived(
		live.overview?.status.last_scan_at ??
			live.runners.reduce<string | null>(
				(a, r) => (r.last_scan_at && (!a || r.last_scan_at > a) ? r.last_scan_at : a),
				null
			)
	);
	// The server's next scan time wins: an idle runner waits 12 h, not the 5 min fallback.
	const nextScanAt = $derived(live.overview?.status.next_scan_at ?? null);
	const emptyMin = $derived(
		nextScanAt ? Math.max(0, Math.ceil((Date.parse(nextScanAt) - now) / 60000)) : nextScanMin(lastScan, now)
	);
	const perm = $derived(t('upnext.permanent', { games: '\u0000' }).split('\u0000'));

	function runner(id: string): Runner | undefined {
		return live.runners.find((r) => r.account_id === id);
	}

	async function refetch(): Promise<void> {
		try {
			live.runners = await api.runners();
		} catch {
			/* the live store reconnects on its own */
		}
	}

	async function commit(id: string, from: number, to: number): Promise<void> {
		failed = false;
		const ok = await commitMove(
			{
				get: () => live.runners,
				set: (r) => (live.runners = r),
				setOrder: (i, ids) => api.setOrder(i, ids),
				fetchRunners: () => api.runners()
			},
			id,
			from,
			to
		);
		if (!ok) failed = true;
	}

	async function reset(): Promise<void> {
		failed = false;
		try {
			await Promise.all(live.runners.filter((r) => r.order_custom).map((r) => api.resetOrder(r.account_id)));
		} catch {
			failed = true;
		}
		await refetch();
	}

	function shown<T>(id: string, items: T[]): T[] {
		return drag && drag.id === id ? moveItem(items, drag.from, drag.to) : items;
	}

	async function mineNext(id: string): Promise<void> {
		if (mineBusy[id]) return;
		mineBusy[id] = true;
		mineFailed[id] = false;
		const ok = await commitMineNow(
			{
				get: () => live.runners,
				set: (r) => (live.runners = r),
				mineNow: (aid) => api.mineNow(aid),
				fetchRunners: () => api.runners()
			},
			id
		);
		mineBusy[id] = false;
		mineFailed[id] = !ok;
	}

	// Why Mine next is unavailable for this account, or null when it can run.
	function mineWhy(q: { canMine: boolean }, r: Runner | undefined): string | null {
		if (!q.canMine) {
			if (r?.state === 'needs_login') return t('home.signed_out_row');
			if (r?.state === 'no_games') return t('home.no_games');
			return t('home.idle_row');
		}
		return null;
	}

	function dragOpts(id: string) {
		return {
			onStart: (from: number) => (drag = { id, from, to: from }),
			onMove: (to: number) => {
				if (drag) drag.to = to;
			},
			onEnd: (from: number, to: number) => {
				drag = null;
				if (from !== to) void commit(id, from, to);
			},
			onCancel: () => (drag = null)
		};
	}
</script>

<section class="panel">
	<div class="panel-h">
		<h2>{t('upnext.title')}</h2><span class="n">{total}</span>
		{#if prefs.priorityMode === 'ending_soonest'}
			<a class="mode" href="/games" title={t('settings.pm_ending_soonest_sub')}>{t('settings.pm_ending_soonest')}</a>
		{/if}
	</div>
	{#if qs.length === 0}
		<p class="empty">
			{lastScan ? t('upnext.empty', { time: etaShort(emptyMin) }) : t('upnext.empty_plain')}
		</p>
	{:else}
		<div class="hint">{t('upnext.hint')}</div>
	{/if}
	{#each qs as q (q.accountId)}
		{@const r = runner(q.accountId)}
		{@const full = shown(q.accountId, q.items)}
		{@const fold = foldQueue(full, open[q.accountId] === true)}
		{@const why = mineWhy(q, r)}
		<div class="acct">
			<PlatformMark platform={q.platform} size="sm" /><span class="pf-name {q.platform}">{q.accountName}</span>
			<button
				class="mine"
				type="button"
				disabled={mineBusy[q.accountId] || why !== null}
				aria-busy={mineBusy[q.accountId] === true}
				title={why ?? t('upnext.mine_next_title', { drop: q.items[0].benefit_name })}
				onclick={() => mineNext(q.accountId)}
			>
				{t('upnext.mine_next')}
			</button>
		</div>
		{#if mineFailed[q.accountId]}<p class="err" role="alert">{t('upnext.mine_next_failed')}</p>{/if}
		<ol class="queue" use:reorder={dragOpts(q.accountId)}>
			{#each fold.rows as item, i (item.benefit_id)}
				{@const after = r ? afterLabel({ ...r, up_next: full }, i) : null}
				<li class:dragging={drag?.id === q.accountId && drag.to === i}>
					<span
						class="grip"
						title={t('upnext.drag')}
						role="presentation"
					>
						<svg viewBox="0 0 12 12" fill="currentColor" aria-hidden="true"
							><circle cx="4" cy="3" r="1" /><circle cx="8" cy="3" r="1" /><circle cx="4" cy="6" r="1" /><circle
								cx="8"
								cy="6"
								r="1"
							/><circle cx="4" cy="9" r="1" /><circle cx="8" cy="9" r="1" /></svg
						>
					</span>
					<span class="qn">{i + 1}</span>
					<div class="qt">
						<div class="d">{item.benefit_name}</div>
						{#if onopen && item.campaign_id}
							<div class="g linked">
								<span>{item.game} ·</span><button
									type="button"
									class="clink"
									data-campaign
									aria-haspopup="dialog"
									title={t('campaign.open')}
									onclick={() => onopen({ platform: item.platform, id: item.campaign_id, highlight: item.benefit_id })}
									>{item.campaign_name}</button
								>
							</div>
						{:else}
							<div class="g">{item.game} · {item.campaign_name}</div>
						{/if}
						<div class="m">
							<span class="pf-name {q.platform}">{q.accountName}</span> ·
							{after
								? t('upnext.meta_after', { min: item.required_minutes, drop: after })
								: t('upnext.meta', { min: item.required_minutes })}
						</div>
					</div>
					<div class="mv">
						{#if i > 0}
							<button type="button" aria-label={t('upnext.move_up')} title={t('upnext.move_up')} onclick={() => commit(q.accountId, i, i - 1)}>
								<svg viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 7.5 6 4.5l3 3" /></svg>
							</button>
						{/if}
					</div>
				</li>
			{/each}
		</ol>
		{#if fold.more > 0}
			<button
				class="more"
				type="button"
				aria-expanded={open[q.accountId] === true}
				onclick={() => (open[q.accountId] = !open[q.accountId])}
			>
				{open[q.accountId] ? t('home.upnext_less') : tn('home.upnext_more', fold.more)}
			</button>
		{/if}
	{/each}
	{#if failed}<p class="err" role="alert">{t('upnext.save_failed')}</p>{/if}
	<div class="panel-f">
		<span>{perm[0]}<a href="/games" class="link">{t('nav.games')}</a>{perm[1] ?? ''}</span>
		{#if custom}<button class="reset" type="button" onclick={reset}>{t('upnext.reset')}</button>{/if}
	</div>
</section>

<style>
	.panel {
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
	}
	.panel-h {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 12px 14px 0;
	}
	h2 {
		margin: 0;
		font-size: 13px;
		font-weight: 600;
	}
	.n {
		font: 12px var(--mono);
		color: var(--t2);
		background: var(--surface-3);
		border-radius: 10px;
		padding: 0 6px;
	}
	.mode {
		margin-left: auto;
		font-size: 12px;
		padding: 1px 8px;
		border-radius: 10px;
		color: var(--accent);
		background: var(--accent-soft);
		white-space: nowrap;
	}
	.mode:hover {
		text-decoration: underline;
	}
	.mode:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.hint {
		padding: 4px 14px 10px;
		font-size: 12px;
		color: var(--t3);
	}
	.empty,
	.err {
		margin: 0;
		padding: 10px 14px;
		font-size: 13px;
		border-top: 1px solid var(--border-soft);
	}
	.empty {
		color: var(--t2);
	}
	.panel-h + .empty {
		margin-top: 10px;
	}
	.err {
		color: var(--red);
		font-size: 12px;
	}
	.acct {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 8px 14px;
		font-size: 12px;
		font-weight: 600;
		color: var(--t2);
		border-top: 1px solid var(--border-soft);
	}
	.queue {
		list-style: none;
		margin: 0;
		padding: 0;
		border-top: 1px solid var(--border-soft);
	}
	.queue li {
		display: grid;
		grid-template-columns: 14px 14px minmax(0, 1fr) 46px;
		gap: 8px;
		align-items: center;
		padding: 10px 8px 10px 10px;
	}
	.queue li + li {
		border-top: 1px solid var(--border-soft);
	}
	.queue li.dragging {
		background: var(--surface-2);
		box-shadow:
			inset 2px 0 0 var(--accent),
			0 6px 18px rgb(0 0 0 / 0.35);
		position: relative;
		z-index: 1;
	}
	.grip {
		color: var(--t3);
		cursor: grab;
		display: grid;
		place-items: center;
		touch-action: none;
	}
	.grip svg {
		width: 12px;
		height: 12px;
	}
	.qn {
		font: 12px var(--mono);
		color: var(--t3);
		text-align: right;
	}
	.qt {
		min-width: 0;
	}
	.d,
	.g,
	.m {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.d {
		font-weight: 500;
		font-size: 13px;
	}
	.g {
		font-size: 12px;
		color: var(--t2);
	}
	.m {
		font-size: 12px;
		color: var(--t3);
		margin-top: 1px;
	}
	/* Each part ellipsizes itself, so the link's focus ring is not clipped. */
	.g.linked {
		display: flex;
		gap: 4px;
		overflow: visible;
	}
	.g.linked > * {
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.g.linked > span {
		flex: none;
		max-width: 50%;
	}
	.clink {
		font: inherit;
		color: inherit;
		text-align: left;
		background: none;
		border: 0;
		padding: 0;
		cursor: pointer;
		white-space: nowrap;
		text-decoration: underline dotted;
		text-decoration-color: var(--t3);
		text-underline-offset: 2px;
	}
	.clink:hover {
		color: var(--text);
		text-decoration-style: solid;
		text-decoration-color: currentColor;
	}
	.clink:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
		border-radius: 3px;
	}
	.mv {
		display: flex;
		justify-content: flex-end;
		gap: 2px;
	}
	.mv button {
		width: 22px;
		height: 22px;
		display: grid;
		place-items: center;
		background: none;
		border: 1px solid transparent;
		border-radius: 4px;
		color: var(--t2);
		cursor: pointer;
	}
	.mv button:disabled {
		opacity: 0.5;
		cursor: default;
	}
	.mv button:hover:not(:disabled) {
		border-color: var(--border);
		color: var(--text);
		background: var(--surface-2);
	}
	.mv svg {
		width: 12px;
		height: 12px;
	}
	.more {
		display: block;
		width: 100%;
		padding: 8px 14px;
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
	.panel-f {
		border-top: 1px solid var(--border-soft);
		padding: 10px 14px;
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 12px;
		color: var(--t3);
	}
	.link {
		color: var(--t2);
	}
	.link:hover {
		color: var(--accent);
	}
	.mine,
	.reset {
		margin-left: auto;
		font-size: 12px;
		color: var(--t2);
		background: none;
		border: 1px solid var(--border);
		border-radius: 6px;
		padding: 2px 8px;
		white-space: nowrap;
		cursor: pointer;
	}
	.mine:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
	.mine[aria-busy='true'] {
		cursor: progress;
	}
	.mine:hover:not(:disabled),
	.reset:hover {
		color: var(--text);
		border-color: var(--t2);
	}
</style>
