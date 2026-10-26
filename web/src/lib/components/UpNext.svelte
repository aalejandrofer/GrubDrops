<script lang="ts">
	import { api } from '$lib/app';
	import { etaShort, nextScanMin } from '$lib/format';
	import { t, tn } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';
	import { afterLabel, anyCustom, commitMove, foldQueue, moveItem, queues } from '$lib/upnext';
	import type { Runner } from '$lib/api/types';
	import PlatformMark from './PlatformMark.svelte';

	const live = getLive();
	let failed = $state(false);
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

	function down(e: PointerEvent, id: string, from: number): void {
		if (e.button !== 0) return;
		(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
		drag = { id, from, to: from };
	}

	function move(e: PointerEvent): void {
		if (!drag) return;
		const ol = (e.currentTarget as HTMLElement).closest('ol');
		if (!ol) return;
		const rows = Array.from(ol.children);
		const y = e.clientY;
		let to = drag.to;
		rows.forEach((li, i) => {
			const b = li.getBoundingClientRect();
			if (y >= b.top && y <= b.bottom) to = i;
		});
		if (y < rows[0].getBoundingClientRect().top) to = 0;
		if (y > rows[rows.length - 1].getBoundingClientRect().bottom) to = rows.length - 1;
		drag.to = to;
	}

	function cancel(): void {
		drag = null;
	}

	function up(): void {
		if (!drag) return;
		const d = drag;
		drag = null;
		void commit(d.id, d.from, d.to);
	}
</script>

<section class="panel">
	<div class="panel-h"><h2>{t('upnext.title')}</h2><span class="n">{total}</span></div>
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
		{#if qs.length > 1}
			<div class="acct"><PlatformMark platform={q.platform} size="sm" /><span class="pf-name {q.platform}">{q.accountName}</span></div>
		{/if}
		<ol class="queue">
			{#each fold.rows as item, i (item.benefit_id)}
				{@const after = r ? afterLabel({ ...r, up_next: full }, i) : null}
				<li class:dragging={drag?.id === q.accountId && drag.to === i}>
					<span
						class="grip"
						title={t('upnext.drag')}
						onpointerdown={(e) => down(e, q.accountId, i)}
						onpointermove={move}
						onpointerup={up}
						onpointercancel={cancel}
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
						<div class="g">{item.game} · {item.campaign_name}</div>
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
		grid-template-columns: 14px 14px minmax(0, 1fr) 22px;
		gap: 8px;
		align-items: center;
		padding: 10px 8px 10px 10px;
	}
	.queue li + li {
		border-top: 1px solid var(--border-soft);
	}
	.queue li.dragging {
		opacity: 0.4;
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
	.mv button:hover {
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
	.reset:hover {
		color: var(--text);
		border-color: var(--t2);
	}
</style>
