<script lang="ts">
	import { api } from '$lib/app';
	import { reorder } from '$lib/drag';
	import { etaShort } from '$lib/format';
	import { campaignLine } from '$lib/games';
	import { t, tn } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';
	import { prefs } from '$lib/prefs.svelte';
	import {
		anyCustom,
		byGame,
		commitGameOrder,
		inGameOrder,
		moveItem,
		queues,
		readGameOrder,
		settleGameOrder,
		writeGameOrder,
		type GameOrder
	} from '$lib/upnext';
	import BoxArt from './BoxArt.svelte';
	import type { CampaignTarget } from './CampaignDrawer.svelte';
	import PlatformMark from './PlatformMark.svelte';

	/** Opens the campaign drawer from a campaign name. */
	let { onopen }: { onopen: (t: CampaignTarget) => void } = $props();

	const live = getLive();
	let failed = $state(false);
	let drag = $state<{ from: number; to: number } | null>(null);
	// The dropped game order, kept for this tab so rows stay where they were dropped.
	let order = $state<GameOrder | null>(readGameOrder());

	function setGameOrder(o: GameOrder | null): void {
		order = o;
		writeGameOrder(o);
	}

	// A reset here or on Home puts the queues back, so the dropped order goes too.
	$effect(() => {
		const next = settleGameOrder(order, live.runners);
		if (next !== order) setGameOrder(next);
	});

	const groups = $derived(inGameOrder(byGame(queues(live.runners)), order?.games ?? null));
	// While a row is dragged, the list previews where it lands.
	const shown = $derived(drag ? moveItem(groups, drag.from, drag.to) : groups);
	const custom = $derived(anyCustom(live.runners));
	const total = $derived(groups.reduce((n, g) => n + g.drops, 0));
	const art = $derived(new Map((live.games?.games ?? []).map((g) => [g.name, g.box_art_url])));
	const perm = $derived(t('inventory.upnext_temporary', { games: '\u0000' }).split('\u0000'));

	async function commit(from: number, to: number): Promise<void> {
		failed = false;
		const games = moveItem(
			groups.map((g) => g.game),
			from,
			to
		);
		setGameOrder({ games, custom: anyCustom(live.runners) });
		const ok = await commitGameOrder(
			{
				get: () => live.runners,
				set: (r) => (live.runners = r),
				setOrder: (i, ids) => api.setOrder(i, ids),
				fetchRunners: () => api.runners()
			},
			games
		);
		if (!ok) failed = true;
	}

	async function reset(): Promise<void> {
		failed = false;
		setGameOrder(null);
		try {
			await Promise.all(live.runners.filter((r) => r.order_custom).map((r) => api.resetOrder(r.account_id)));
		} catch {
			failed = true;
		}
		try {
			live.runners = await api.runners();
		} catch {
			/* the live store reconnects on its own */
		}
	}

	// Only the row header drags, so campaign names stay clickable.
	const dragOpts = {
		items: (list: HTMLElement) => Array.from(list.querySelectorAll<HTMLElement>(':scope > li > .head')),
		onStart: (from: number) => (drag = { from, to: from }),
		onMove: (to: number) => {
			if (drag) drag.to = to;
		},
		onEnd: (from: number, to: number) => {
			drag = null;
			if (from !== to) void commit(from, to);
		},
		onCancel: () => (drag = null)
	};
</script>

<section class="panel">
	<div class="panel-h">
		<h2>{t('upnext.title')}</h2><span class="n">{total}</span>
		<span class="sub">{tn('inventory.upnext_games', groups.length)}</span>
		{#if prefs.priorityMode === 'ending_soonest'}
			<a class="mode" href="/games" title={t('settings.pm_ending_soonest_sub')}>{t('settings.pm_ending_soonest')}</a>
		{/if}
	</div>
	<ol class="rows" use:reorder={dragOpts}>
		{#each shown as g, i (g.game)}
			<li class:dragging={drag?.to === i}>
				<div class="head">
					<span class="grip" title={t('upnext.drag')} role="presentation">
						<svg viewBox="0 0 12 12" fill="currentColor" aria-hidden="true"
							><circle cx="4" cy="3" r="1" /><circle cx="8" cy="3" r="1" /><circle cx="4" cy="6" r="1" /><circle
								cx="8"
								cy="6"
								r="1"
							/><circle cx="4" cy="9" r="1" /><circle cx="8" cy="9" r="1" /></svg
						>
					</span>
					<div class="row">
						<span class="qn">{i + 1}</span>
						<span class="art"><BoxArt src={art.get(g.game) ?? null} name={g.game} size="sm" /></span>
						<span class="what">
							<span class="gn">{g.game} <span class="cnt">· {tn('games.drops_open', g.drops)}</span></span>
							<span class="meta">
								{tn('inventory.upnext_campaigns', g.campaigns.length)} ·
								{#each g.accounts as a, j (a.id)}{j ? ', ' : ''}<span class="pf-name {a.platform}">{a.name}</span>{/each}
							</span>
						</span>
						<span class="left"><b>{etaShort(g.minutesLeft)}</b><small>{t('inventory.upnext_to_watch')}</small></span>
					</div>
				</div>
				<div class="inline">
					{#each g.campaigns as c (`${c.platform}:${c.id}`)}
						<div class="camp">
							<div class="ch">
								<PlatformMark platform={c.platform} size="sm" />
								{#if c.id}
									<button
										type="button"
										class="cn clink"
										aria-haspopup="dialog"
										title={t('campaign.open')}
										onclick={() => onopen({ platform: c.platform, id: c.id, highlight: c.drops[0].target.benefit_id })}
										>{c.name}</button
									>
								{:else}
									<span class="cn">{c.name}</span>
								{/if}
								<span class="ce">{campaignLine({ id: c.id, platform: c.platform, name: c.name, drops: c.drops.length, ends_at: c.endsAt }, prefs.zone)}</span>
							</div>
						</div>
					{/each}
				</div>
			</li>
		{/each}
	</ol>
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
		overflow: hidden;
	}
	.panel-h {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 12px 14px 10px;
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
	.sub {
		font-size: 12px;
		color: var(--t3);
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
	.rows {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.rows > li {
		border-top: 1px solid var(--border-soft);
	}
	.rows > li.dragging {
		background: var(--surface-2);
		box-shadow: 0 6px 18px rgb(0 0 0 / 0.35);
		position: relative;
		z-index: 1;
	}
	.head {
		display: grid;
		grid-template-columns: 14px minmax(0, 1fr);
		align-items: center;
		padding-left: 8px;
	}
	.grip {
		color: var(--t3);
		cursor: grab;
		display: grid;
		place-items: center;
		align-self: stretch;
		touch-action: none;
	}
	.grip svg {
		width: 12px;
		height: 12px;
	}
	.row {
		display: grid;
		grid-template-columns: 18px 36px minmax(0, 1fr) auto;
		align-items: center;
		gap: 12px;
		width: 100%;
		padding: 10px 14px 10px 6px;
	}
	.qn {
		font: 12px var(--mono);
		color: var(--t3);
		text-align: right;
	}
	.art {
		width: 36px;
	}
	.what {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}
	.gn,
	.meta {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.gn {
		font-size: 13px;
		font-weight: 600;
	}
	.cnt {
		font-weight: 400;
		color: var(--t2);
	}
	.meta {
		font-size: 12px;
		color: var(--t3);
	}
	.left {
		display: flex;
		flex-direction: column;
		align-items: flex-end;
		gap: 2px;
		white-space: nowrap;
	}
	.left b {
		font: 500 12.5px var(--mono);
	}
	.left small {
		font-size: 11px;
		color: var(--t3);
	}
	.inline {
		padding: 2px 14px 12px 80px;
	}
	.camp + .camp {
		margin-top: 6px;
	}
	.ch {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
		font-size: 12.5px;
	}
	.cn {
		font-weight: 600;
	}
	.ce {
		color: var(--t3);
		font-size: 12px;
	}
	.clink {
		font: inherit;
		font-weight: 600;
		color: inherit;
		text-align: left;
		background: none;
		border: 0;
		padding: 0;
		cursor: pointer;
		text-decoration: underline dotted;
		text-decoration-color: var(--t3);
		text-underline-offset: 2px;
	}
	.clink:hover {
		text-decoration-style: solid;
		text-decoration-color: currentColor;
	}
	.clink:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
		border-radius: 3px;
	}
	.err {
		margin: 0;
		padding: 10px 14px;
		font-size: 12px;
		color: var(--red);
		border-top: 1px solid var(--border-soft);
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
	.link {
		color: var(--t2);
	}
	.link:hover {
		color: var(--accent);
	}
	@media (max-width: 560px) {
		.qn {
			display: none;
		}
		.head {
			padding-left: 6px;
		}
		.row {
			grid-template-columns: 36px minmax(0, 1fr) auto;
			padding: 10px 12px 10px 6px;
		}
		.inline {
			padding: 2px 12px 12px;
		}
	}
</style>
