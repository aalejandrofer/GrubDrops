<script lang="ts">
	import { onDestroy, untrack } from 'svelte';
	import { api } from '$lib/app';
	import type { Claim, ClaimsQuery, ClaimsResp } from '$lib/api/types';
	import UpNextGames from '$lib/components/UpNextGames.svelte';
	import ArmButton from '$lib/components/ArmButton.svelte';
	import BoxArt from '$lib/components/BoxArt.svelte';
	import CopyCommand from '$lib/components/CopyCommand.svelte';
	import CampaignDrawer, { type CampaignTarget } from '$lib/components/CampaignDrawer.svelte';
	import PageHead from '$lib/components/PageHead.svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { claimStamp, etaShort, formatInt, relTime } from '$lib/format';
	import { t, tn } from '$lib/i18n/index.svelte';
	import { queues } from '$lib/upnext';
	import { getLive } from '$lib/live-context';
	import { prefs } from '$lib/prefs.svelte';
	import { inProgress, pageCount, pageQuery, pageRange, PAGE_SIZES, readPageSize, validPageSize, writePageSize } from '$lib/inventory';

	const live = getLive();

	let drawer = $state<CampaignTarget | null>(null);

	function openCampaign(c: Claim): void {
		if (c.campaign_id) drawer = { platform: c.platform, id: c.campaign_id, highlight: c.benefit_id };
	}

	/** A click anywhere on a row opens its campaign, except on the row's own controls. */
	function onRowClick(e: MouseEvent, c: Claim): void {
		if (e.target instanceof Element && e.target.closest('button, a, input')) return;
		openCampaign(c);
	}

	let now = $state(Date.now());
	$effect(() => {
		const id = setInterval(() => (now = Date.now()), 5000);
		return () => clearInterval(id);
	});

	let game = $state('');
	let account = $state('');
	let search = $state('');
	let q = $state('');
	let page = $state(1);
	let size = $state(readPageSize());
	let timer: ReturnType<typeof setTimeout> | undefined;
	let items = $state<Claim[]>([]);
	let total = $state(0);
	let games = $state<string[]>([]);
	let accts = $state<{ id: string; name: string }[]>([]);
	let loaded = $state(false);
	let busy = $state(false);
	let failed = $state(false);
	let seq = 0;

	const filter = $derived<ClaimsQuery>({ game: game || undefined, account: account || undefined, q: q || undefined });
	const pages = $derived(pageCount(total, size));
	const range = $derived(pageRange(page, size, total));
	const progress = $derived(inProgress(live.runners));
	const qs = $derived(queues(live.runners));
	const queuedTotal = $derived(qs.reduce((n, x) => n + x.items.length, 0));
	const kpi = $derived(live.overview?.kpis);
	const head = $derived(
		kpi ? t('inventory.head', { today: formatInt(kpi.claimed_today), total: formatInt(kpi.claimed_all_time) }) : undefined
	);
	const stamp = $derived(
		live.updatedAt === null
			? undefined
			: t('home.updated', { rel: relTime(new Date(live.updatedAt).toISOString(), now) })
	);

	const key = (c: Claim) => `${c.account_id}:${c.benefit_id}`;

	function apply(r: ClaimsResp): void {
		const seen = new Set<string>();
		items = r.items.filter((c) => !seen.has(key(c)) && seen.add(key(c)));
		total = r.total;
		games = r.games;
		accts = r.accounts;
	}

	/** Out-of-order responses are dropped: only the latest request lands. */
	async function load(): Promise<void> {
		const mine = ++seq;
		busy = true;
		failed = false;
		try {
			const r = await api.claims(pageQuery(filter, page, size));
			if (mine !== seq) return;
			if (r.items.length === 0 && r.total > 0 && page > 1) {
				page = pageCount(r.total, size);
				void load();
				return;
			}
			apply(r);
			loaded = true;
		} catch {
			if (mine !== seq) return;
			items = [];
			total = 0;
			failed = true;
		} finally {
			if (mine === seq) busy = false;
		}
	}

	function goto(p: number): void {
		page = Math.min(Math.max(1, p), pages);
		void load();
	}

	function onSearch(): void {
		clearTimeout(timer);
		timer = setTimeout(() => {
			if (search.trim() === q) return;
			q = search.trim();
			page = 1;
			void load();
		}, 250);
	}

	function pickSize(e: Event): void {
		size = validPageSize(Number((e.currentTarget as HTMLSelectElement).value));
		writePageSize(size);
		page = 1;
		void load();
	}

	onDestroy(() => clearTimeout(timer));

	function pick(kind: 'game' | 'account', v: string): void {
		if (kind === 'game') game = v;
		else account = v;
		page = 1;
		void load();
	}

	$effect(() => {
		untrack(() => {
			void load();
		});
	});

	// New claims from the live feed land at the top while their filters match.
	$effect(() => {
		const recent = live.recent;
		untrack(() => {
			if (!loaded || page !== 1 || search.trim() !== '' || q !== '') return;
			const have = new Set(items.map(key));
			const fresh = recent
				.filter((c) => !have.has(key(c)))
				.filter((c) => (!game || c.game === game) && (!account || c.account_id === account))
				.filter((c) => items.length === 0 || Date.parse(c.claimed_at) >= Date.parse(items[0].claimed_at));
			if (fresh.length === 0) return;
			items = [...fresh, ...items].slice(0, size);
			total += fresh.length;
		});
	});

	function platformOf(id: string) {
		return live.accounts.find((a) => a.id === id)?.platform ?? null;
	}
	const known = (id: string) => live.accounts.some((a) => a.id === id);
	const removed = (c: Claim) => c.account_name === '' || (live.accounts.length > 0 && !known(c.account_id));
	const anyFilter = $derived(game !== '' || account !== '');

	// Row action: mark a claim uncollected (ArmButton asks for a second click).
	let acting = $state<Record<string, boolean>>({});
	let actErr = $state('');

	async function removeClaim(c: Claim): Promise<void> {
		const k = key(c);
		acting[k] = true;
		actErr = '';
		try {
			await api.removeClaim(c.account_id, c.benefit_id);
			items = items.filter((x) => key(x) !== k);
			live.recent = live.recent.filter((x) => key(x) !== k);
			total = Math.max(0, total - 1);
		} catch {
			actErr = t('mark.uncollect_failed');
		}
		acting[k] = false;
	}
	const searching = $derived(q !== '');
</script>

<PageHead title={t('nav.inventory')} status={head} lead={stamp}>
	{#snippet action()}
		<span class="invlinks"
			>{#each t('inventory.links', { twitch: '{twitch}', kick: '{kick}' }).split(/(\{twitch\}|\{kick\})/) as part, i (i)}{#if part === '{twitch}'}<a
						class="tw"
						href="https://www.twitch.tv/drops/inventory"
						target="_blank"
						rel="noopener">{t('inventory.link_twitch')}</a
					>{:else if part === '{kick}'}<a class="ki" href="https://kick.com/drops/all-campaigns" target="_blank" rel="noopener"
						>{t('inventory.link_kick')}</a
					>{:else}{part}{/if}{/each}</span
		>
	{/snippet}
	{#if progress.length > 0}
		<section class="sec" aria-label={t('inventory.in_progress')}>
			<div class="sec-h">
				<h2>
					<svg viewBox="0 0 16 16" fill="none" stroke="var(--accent)" stroke-width="1.6" aria-hidden="true"
						><circle cx="8" cy="8" r="6" stroke="var(--surface-3)" /><path d="M8 2a6 6 0 0 1 6 6" stroke-linecap="round" /></svg
					>{t('inventory.in_progress')} <span class="n">{progress.length}</span>
				</h2>
			</div>
			<div class="panel">
				{#each progress as p (p.runner.account_id)}
					<div class="prog">
						<div class="what">
							<div class="t">{p.target.benefit_name}</div>
							<div class="c">
								<span>{p.target.game} · {p.target.campaign_name}</span>
								<span class="acct"><PlatformMark platform={p.runner.platform} size="sm" />{p.runner.account_name}</span>
							</div>
						</div>
						<div class="bw">
							<div class="num">
								<span>{t('inventory.min_of', { m: p.target.minutes, req: p.target.required_minutes })}</span>
								<span>{p.pct}%</span>
							</div>
							<div class="bar"><i style="width:{p.pct}%"></i></div>
						</div>
						<div class="eta">
							{t('inventory.eta', { eta: etaShort(p.left) })}<span
								>{t('inventory.eta_on', { account: p.runner.account_name })}</span
							>
						</div>
					</div>
				{/each}
			</div>
		</section>
	{/if}

	{#if queuedTotal > 0}
		<section class="sec" aria-label={t('upnext.title')}><UpNextGames onopen={(c) => (drawer = c)} /></section>
	{/if}

	<section class="sec" aria-label={t('inventory.claimed')}>
		<div class="sec-h">
			<h2>
				<svg viewBox="0 0 16 16" fill="none" stroke="var(--green)" stroke-width="1.6" aria-hidden="true"
					><circle cx="8" cy="8" r="6" /><path d="m5.3 8.2 1.8 1.8 3.6-3.8" stroke-linecap="round" stroke-linejoin="round" /></svg
				>{t('inventory.claimed')} <span class="n">{formatInt(total)}</span>
			</h2>
		</div>
		{#if games.length > 0 || accts.length > 0 || anyFilter || searching || total > 0}
			<div class="filters">
				<input
					class="search"
					type="search"
					placeholder={t('inventory.search_placeholder')}
					aria-label={t('inventory.search_placeholder')}
					bind:value={search}
					oninput={onSearch}
				/>
				<div class="fgroup" role="group" aria-label={t('inventory.filter_game')}>
					<span>{t('inventory.filter_game')}</span>
					<button type="button" class="chip" class:on={game === ''} aria-pressed={game === ''} onclick={() => pick('game', '')}
						>{t('inventory.all')}</button
					>
					{#each games as g (g)}
						<button type="button" class="chip" class:on={game === g} aria-pressed={game === g} onclick={() => pick('game', g)}>{g}</button>
					{/each}
				</div>
				<div class="fgroup" role="group" aria-label={t('inventory.filter_account')}>
					<span>{t('inventory.filter_account')}</span>
					<button type="button" class="chip" class:on={account === ''} aria-pressed={account === ''} onclick={() => pick('account', '')}
						>{t('inventory.all')}</button
					>
					{#each accts as a (a.id)}
						{@const pf = platformOf(a.id)}
						<button type="button" class="chip" class:on={account === a.id} aria-pressed={account === a.id} onclick={() => pick('account', a.id)}
							>{#if pf}<PlatformMark platform={pf} size="sm" />{/if}{a.name}</button
						>
					{/each}
				</div>
			</div>
		{/if}
		<div class="panel">
			{#if items.length > 0}
				<div class="hist-h">
					<span>{t('inventory.col_reward')}</span><span>{t('inventory.col_game')}</span><span>{t('inventory.col_account')}</span><span class="tmh"
						>{t('inventory.col_claimed')}</span
					><span></span>
				</div>
				{#each items as c (key(c))}
					{@const pf = platformOf(c.account_id)}
					<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -- the reward name button is the keyboard path -->
					<div class="hrow" class:open={!!c.campaign_id} data-campaign={c.campaign_id ? '' : undefined} onclick={(e) => onRowClick(e, c)}>
						<div class="rw">
							<span class="img"><BoxArt src={c.image_url || null} name={c.benefit_name} size="sm" /></span>
							<span class="rwt">
								{#if c.campaign_id}
									<button type="button" class="nm" aria-haspopup="dialog" title={t('campaign.open')} onclick={() => openCampaign(c)}
										>{c.benefit_name}</button
									>
								{:else}
									<span class="nm">{c.benefit_name}</span>
								{/if}
								{#if c.source === 'manual'}<span class="tag">{t('inventory.manual')}</span>{/if}
								{#if c.code}<CopyCommand command={c.code} />{/if}
							</span>
						</div>
						<span class="gm">{c.game}</span>
						<span class="ac">
							{#if removed(c)}
								<span class="gone">{t('inventory.removed_account')}</span>
							{:else}
								<span class="acct">{#if pf}<PlatformMark platform={pf} size="sm" />{/if}{c.account_name}</span>
							{/if}
						</span>
						<span class="tm">{claimStamp(c.claimed_at, now, prefs.zone, prefs.fmt)}</span>
						<span class="rm"
							><ArmButton
								kind="uncollect"
								drop={c.benefit_name}
								account={c.account_name || t('inventory.removed_account')}
								disabled={acting[key(c)]}
								onconfirm={() => removeClaim(c)}
							/></span
						>
					</div>
				{/each}
			{:else if loaded && !busy && !failed}
				<div class="empty">{searching ? t('inventory.no_match') : anyFilter ? t('inventory.empty_filtered') : t('inventory.empty')}</div>
			{/if}
			{#if actErr}<div class="empty err" role="alert">{actErr}</div>{/if}
			{#if failed}
				<div class="empty err" role="status">
					{t('inventory.failed')}
					<button type="button" class="btn" disabled={busy} onclick={() => load()}>{t('inventory.retry')}</button>
				</div>
			{/if}
			{#if total > PAGE_SIZES[0]}
				<div class="hist-foot">
					<span class="rng">{t('inventory.range', { from: formatInt(range.from), to: formatInt(range.to), total: formatInt(total) })}</span>
					<label class="per">
						<span>{t('inventory.per_page')}</span>
						<select value={size} onchange={pickSize} disabled={busy}>
							{#each PAGE_SIZES as n}<option value={n}>{n}</option>{/each}
						</select>
					</label>
					{#if pages > 1}
						<div class="nav">
							<button type="button" class="btn" disabled={busy || page <= 1} onclick={() => goto(page - 1)}>{t('inventory.prev')}</button>
							<span>{t('inventory.page_of', { page: formatInt(page), pages: formatInt(pages) })}</span>
							<button type="button" class="btn" disabled={busy || page >= pages} onclick={() => goto(page + 1)}>{t('inventory.next')}</button>
						</div>
					{/if}
				</div>
			{/if}
		</div>
	</section>
</PageHead>

<CampaignDrawer target={drawer} onclose={() => (drawer = null)} />

<style>
	.invlinks {
		font-size: 13px;
		font-weight: 500;
		color: var(--t2);
	}
	.invlinks a {
		text-decoration: none;
	}
	.invlinks a:hover {
		text-decoration: underline;
	}
	.invlinks a:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.invlinks .tw {
		color: var(--twitch-fg);
	}
	.invlinks .ki {
		color: var(--kick-fg);
	}
	.sec {
		margin-top: 28px;
		min-width: 0;
	}
	.sec:first-child {
		margin-top: 0;
	}
	.sec-h {
		display: flex;
		align-items: center;
		gap: 8px;
		margin-bottom: 8px;
		min-height: 26px;
		flex-wrap: wrap;
	}
	.sec-h h2 {
		margin: 0;
		font-size: 13px;
		font-weight: 600;
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.sec-h h2 svg {
		width: 14px;
		height: 14px;
		flex: none;
	}
	.n {
		font: 400 12px var(--mono);
		color: var(--t2);
		background: var(--surface-3);
		border-radius: 10px;
		padding: 0 6px;
	}
	.panel {
		background: var(--surface);
		border: 1px solid var(--border-soft);
		border-radius: var(--radius);
		overflow: hidden;
	}
	.panel > :global(* + *) {
		border-top: 1px solid var(--border-soft);
	}
	.acct {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font-size: 12px;
		color: var(--text);
		background: var(--surface-3);
		border-radius: 4px;
		padding: 1px 6px 1px 5px;
		white-space: nowrap;
		max-width: 100%;
	}
	.gone {
		font-size: 12px;
		color: var(--t3);
	}
	.bar {
		height: 4px;
		border-radius: 2px;
		background: var(--surface-3);
		overflow: hidden;
	}
	.bar i {
		display: block;
		height: 100%;
		background: var(--accent);
		border-radius: 2px;
		transition: width 0.8s ease;
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
	}
	.btn:hover:not([disabled]) {
		border-color: var(--t3);
		background: var(--surface-3);
	}
	.btn[disabled] {
		opacity: 0.45;
		cursor: default;
	}
	.btn:focus-visible,
	.chip:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}

	.prog {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 240px 110px;
		gap: 16px;
		align-items: center;
		padding: 12px 14px;
	}
	.prog .t {
		font-size: 13px;
		font-weight: 500;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.prog .c {
		font-size: 12px;
		color: var(--t2);
		display: flex;
		align-items: center;
		gap: 8px;
		margin-top: 3px;
		flex-wrap: wrap;
	}
	.prog .num {
		font: 12px var(--mono);
		color: var(--t3);
		display: flex;
		justify-content: space-between;
		margin-bottom: 5px;
	}
	.prog .eta {
		text-align: right;
		font-size: 13px;
		font-weight: 500;
	}
	.prog .eta span {
		display: block;
		font-size: 12px;
		color: var(--t3);
		font-weight: 400;
	}
	.tag {
		font-size: 11px;
		color: var(--t2);
		background: var(--surface-3);
		border-radius: 10px;
		padding: 0 6px;
		flex: none;
	}
	.rm {
		display: grid;
		justify-items: end;
	}

	.filters {
		display: flex;
		flex-wrap: wrap;
		gap: 6px 16px;
		margin-bottom: 10px;
	}
	.fgroup {
		display: flex;
		align-items: center;
		gap: 4px;
		flex-wrap: wrap;
	}
	.fgroup > span {
		font-size: 12px;
		color: var(--t3);
		text-transform: uppercase;
		letter-spacing: 0.06em;
		font-weight: 600;
		margin-right: 4px;
	}
	.chip {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font-size: 12px;
		color: var(--t2);
		background: transparent;
		border: 1px solid var(--border);
		border-radius: 999px;
		padding: 2px 10px;
	}
	.chip:hover {
		color: var(--text);
		border-color: var(--t3);
	}
	.chip.on {
		color: var(--text);
		background: var(--accent-soft);
		border-color: color-mix(in srgb, var(--accent) 50%, transparent);
	}

	.hist-h,
	.hrow {
		display: grid;
		grid-template-columns: minmax(0, 1.8fr) minmax(0, 1fr) minmax(0, 1fr) 110px 22px;
		gap: 12px;
		align-items: center;
		padding: 8px 14px;
	}
	.hist-h {
		font-size: 12px;
		color: var(--t3);
		text-transform: uppercase;
		letter-spacing: 0.06em;
		font-weight: 600;
		background: var(--surface-2);
	}
	.hist-h .tmh,
	.hrow .tm {
		text-align: right;
	}
	.hrow {
		font-size: 13px;
	}
	.hrow:hover {
		background: var(--surface-2);
	}
	.rw {
		display: flex;
		align-items: center;
		gap: 10px;
		min-width: 0;
	}
	.img {
		flex: none;
		width: 32px;
		height: 32px;
		border-radius: 4px;
		overflow: hidden;
		display: block;
	}
	.img :global(.art) {
		min-height: 0;
	}
	.rwt {
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-width: 0;
		align-items: flex-start;
	}
	.nm {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		font-weight: 500;
		max-width: 100%;
	}
	button.nm {
		padding: 0;
		border: 0;
		background: none;
		color: inherit;
		font: inherit;
		font-weight: 500;
		text-align: left;
		cursor: pointer;
	}
	button.nm:hover {
		text-decoration: underline;
	}
	button.nm:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.hrow.open {
		cursor: pointer;
	}
	.gm {
		color: var(--t2);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.ac {
		min-width: 0;
		display: flex;
	}
	.tm {
		font: 12px var(--mono);
		color: var(--t3);
		white-space: nowrap;
	}
	.empty {
		padding: 22px 14px;
		text-align: center;
		font-size: 13px;
		color: var(--t3);
	}
	.empty.err {
		color: var(--red);
	}
	.search {
		flex: 1 1 100%;
		min-width: 0;
		max-width: 320px;
		font-size: 13px;
		color: var(--text);
		background: var(--surface-2);
		border: 1px solid var(--border);
		border-radius: 6px;
		padding: 4px 10px;
	}
	.search:focus-visible,
	.per select:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.per {
		display: inline-flex;
		align-items: center;
		gap: 6px;
	}
	.per select {
		font-size: 12px;
		color: var(--text);
		background: var(--surface-2);
		border: 1px solid var(--border);
		border-radius: 6px;
		padding: 2px 6px;
	}
	.nav {
		display: inline-flex;
		align-items: center;
		gap: 10px;
		margin-left: auto;
	}
	.hist-foot {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 8px 16px;
		padding: 10px 14px;
		font-size: 12px;
		color: var(--t3);
		background: var(--surface-2);
	}

	@media (max-width: 720px) {
		.search {
			font-size: 16px;
		}
		.prog {
			grid-template-columns: minmax(0, 1fr) auto;
			grid-template-areas: 'what eta' 'bar bar';
			row-gap: 10px;
		}
		.prog .what {
			grid-area: what;
			min-width: 0;
		}
		.prog .eta {
			grid-area: eta;
		}
		.prog .bw {
			grid-area: bar;
		}
		.hist-h {
			display: none;
		}
		.hrow {
			grid-template-columns: minmax(0, 1fr) auto 22px;
			grid-template-areas: 'rw tm rm' 'gm ac ac';
			row-gap: 4px;
			column-gap: 10px;
		}
		.rw {
			grid-area: rw;
		}
		.hrow .tm {
			grid-area: tm;
		}
		.hrow .gm {
			grid-area: gm;
			font-size: 12px;
		}
		.hrow .ac {
			grid-area: ac;
			justify-self: end;
		}
		.rm {
			grid-area: rm;
		}
	}
</style>
