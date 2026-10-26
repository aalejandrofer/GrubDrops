<script lang="ts">
	import { api } from '$lib/app';
	import { nextScanMin } from '$lib/format';
	import { t, tn } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';
	import { afterLabel, anyCustom, moveItem, queues } from '$lib/upnext';
	import type { Runner } from '$lib/api/types';
	import PlatformMark from './PlatformMark.svelte';

	const live = getLive();
	let failed = $state(false);
	let drag = $state<{ id: string; from: number; to: number } | null>(null);
	let now = $state(Date.now());
	$effect(() => {
		const id = setInterval(() => (now = Date.now()), 30000);
		return () => clearInterval(id);
	});

	const qs = $derived(queues(live.runners));
	const total = $derived(qs.reduce((n, q) => n + q.items.length, 0));
	const custom = $derived(anyCustom(live.runners));
	// The scan times come from the runners; the latest one is the last full scan.
	const lastScan = $derived(
		live.runners.reduce<string | null>(
			(a, r) => (r.last_scan_at && (!a || r.last_scan_at > a) ? r.last_scan_at : a),
			null
		)
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
		const r = runner(id);
		if (!r || from === to) return;
		const next = moveItem(r.up_next, from, to);
		if (next.every((x, i) => x === r.up_next[i])) return;
		failed = false;
		live.runners = live.runners.map((x) =>
			x.account_id === id ? { ...x, up_next: next, order_custom: true } : x
		);
		try {
			await api.setOrder(id, next.map((x) => x.benefit_id));
		} catch {
			failed = true;
			await refetch();
		}
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

	function up(): void {
		if (!drag) return;
		const d = drag;
		drag = null;
		void commit(d.id, d.from, d.to);
	}
</script>

<section class="panel">
	<div class="panel-h"><h2>{t('upnext.title')}</h2><span class="n">{total}</span></div>
	<div class="hint">{t('upnext.hint')}</div>
	{#if qs.length === 0}
		<p class="empty">{tn('upnext.empty', nextScanMin(lastScan, now), {})}</p>
	{/if}
	{#each qs as q (q.accountId)}
		{@const r = runner(q.accountId)}
		{#if qs.length > 1}
			<div class="acct"><PlatformMark platform={q.platform} size="sm" />{q.accountName}</div>
		{/if}
		<ol class="queue">
			{#each shown(q.accountId, q.items) as item, i (item.benefit_id)}
				{@const after = r ? afterLabel({ ...r, up_next: shown(q.accountId, q.items) }, i) : null}
				<li class:dragging={drag?.id === q.accountId && drag.to === i}>
					<span
						class="grip"
						title={t('upnext.drag')}
						onpointerdown={(e) => down(e, q.accountId, i)}
						onpointermove={move}
						onpointerup={up}
						onpointercancel={up}
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
							{after
								? t('upnext.meta_after', { account: q.accountName, min: item.required_minutes, drop: after })
								: t('upnext.meta', { account: q.accountName, min: item.required_minutes })}
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
