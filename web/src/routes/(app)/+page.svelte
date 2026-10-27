<script lang="ts">
	import { goto } from '$app/navigation';
	import CampaignDrawer, { type CampaignTarget } from '$lib/components/CampaignDrawer.svelte';
	import GameCard from '$lib/components/GameCard.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import PageHead from '$lib/components/PageHead.svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import UpNext from '$lib/components/UpNext.svelte';
	import { clock, formatInt, relTime, shortDate } from '$lib/format';
	import { gameCards, groupClaims, homeView, notMining, visibleCards, fitCount } from '$lib/home';
	import { t, tn } from '$lib/i18n/index.svelte';
	import { getLive, getNet } from '$lib/live-context';
	import { wantsWizard } from '$lib/onboarding';
	import { prefs } from '$lib/prefs.svelte';

	const live = getLive();
	const net = getNet();
	let retrying = $state(false);
	let drawer = $state<CampaignTarget | null>(null);

	let now = $state(Date.now());
	$effect(() => {
		const id = setInterval(() => (now = Date.now()), 5000);
		return () => clearInterval(id);
	});

	const o = $derived(live.overview);
	// Offline keeps the last data on screen; the body follows what that data says.
	const mode = $derived(
		homeView({
			overview: o,
			games: live.games?.games ?? null,
			overviewFailed: live.failed.overview,
			gamesFailed: live.failed.games,
			unreachable: live.unreachable || net.down,
			forceWatching: live.runners.some((r) => r.state === 'force_watching')
		})
	);
	// First run, no accounts yet: the wizard replaces Home.
	$effect(() => {
		if (wantsWizard(mode)) void goto('/welcome', { replaceState: true });
	});
	const dashboard = $derived(
		mode === 'healthy' || mode === 'nothing_live' || mode === 'signed_out' || mode === 'paused'
	);
	const lastScan = $derived(live.games?.last_scan_at ?? o?.status.last_scan_at ?? null);
	const nextScan = $derived(live.games?.next_scan_at ?? o?.status.next_scan_at ?? null);
	const pausedIds = $derived(new Set(live.accounts.filter((a) => !a.enabled).map((a) => a.id)));
	const cards = $derived(gameCards(live.games?.games ?? [], live.runners, pausedIds));
	const idle = $derived(notMining(live.accounts, live.runners));
	const IDLE_KEYS = {
		idle: 'accounts.status.idle',
		signed_out: 'accounts.status.signed_out',
		paused: 'accounts.status.paused'
	} as const;
	// Wide layout: render every card, measure them, show as many as fit beside
	// the right column. Cards past the fit stay in the DOM, out of flow, so
	// their heights stay measurable. Narrow layout: fixed cap.
	const MAX_NARROW = 7;
	const CARD_GAP = 12;
	const MORE_LINE = 46;
	let cardsEl = $state<HTMLElement | null>(null);
	let asideEl = $state<HTMLElement | null>(null);
	let fit = $state<number | null>(null);
	const ordered = $derived(visibleCards(cards, Infinity).shown);
	let expanded = $state(false);
	const fitShown = $derived(Math.min(ordered.length, fit ?? MAX_NARROW));
	const hiddenCount = $derived(cards.length - fitShown);
	const shownCount = $derived(expanded ? ordered.length : fitShown);
	const listed = $derived(fit === null ? ordered.slice(0, shownCount) : ordered);

	$effect(() => {
		if (!cardsEl || !asideEl) return;
		const list = cardsEl;
		const side = asideEl;
		const mq = window.matchMedia('(max-width: 960px)');
		let raf = 0;
		const measure = () => {
			raf = 0;
			if (mq.matches) {
				fit = null;
				return;
			}
			const arts = [...list.querySelectorAll<HTMLElement>(':scope > article.game')];
			if (arts.length === 0) return;
			const chips = list.querySelector<HTMLElement>(':scope > .not-mining');
			const avail = side.offsetHeight - arts[0].offsetTop - (chips ? chips.offsetHeight + CARD_GAP : 0);
			const heights = arts.map((a) => a.offsetHeight);
			let n = fitCount(heights, CARD_GAP, avail, 2);
			if (n < heights.length) n = fitCount(heights, CARD_GAP, avail - MORE_LINE, 2);
			fit = n;
		};
		const schedule = () => {
			if (!raf) raf = requestAnimationFrame(measure);
		};
		const ro = new ResizeObserver(schedule);
		const watch = () => {
			ro.disconnect();
			ro.observe(side);
			ro.observe(list);
			list.querySelectorAll(':scope > article.game').forEach((a) => ro.observe(a));
		};
		const mo = new MutationObserver(() => {
			watch();
			schedule();
		});
		mo.observe(list, { childList: true });
		mq.addEventListener('change', schedule);
		watch();
		measure();
		return () => {
			ro.disconnect();
			mo.disconnect();
			mq.removeEventListener('change', schedule);
			if (raf) cancelAnimationFrame(raf);
		};
	});
	const groups = $derived(groupClaims(live.recent, now, prefs.zone));
	const stamp = $derived(
		live.updatedAt === null
			? undefined
			: t('home.updated', { rel: relTime(new Date(live.updatedAt).toISOString(), now) })
	);
	const kpi = $derived(o?.kpis);
	const unhealthy = $derived(o && kpi ? kpi.accounts_healthy < o.status.accounts_total : false);
	const force = $derived(o?.force_watch ?? null);
	const sections = $derived([
		{ key: 'today', label: t('home.today'), items: groups.today },
		{ key: 'yesterday', label: t('home.yesterday'), items: groups.yesterday },
		{ key: 'earlier', label: t('home.earlier'), items: groups.earlier }
	]);

	async function retry(): Promise<void> {
		retrying = true;
		try {
			await live.refetchAll();
		} finally {
			retrying = false;
		}
	}

	function when(iso: string, bucket: string): string {
		return bucket === 'earlier' ? shortDate(iso, prefs.zone) : clock(iso, prefs.zone, prefs.fmt);
	}
</script>

<PageHead title={t('nav.home')} stamp={dashboard ? stamp : undefined}>
	{#if mode === 'offline'}
		{#if !o}
			<div class="empty err" role="alert">
				<span>{t('home.offline')}</span>
				<button type="button" class="btn" disabled={retrying} onclick={retry}>{t('inventory.retry')}</button>
			</div>
		{/if}
	{:else if mode === 'loading' || mode === 'first_run'}
		<p class="none">{t('home.loading')}</p>
	{:else if mode === 'error'}
		<div class="empty err" role="alert">
			<span>{t('home.load_failed')}</span>
			<button type="button" class="btn" disabled={retrying} onclick={retry}>{t('inventory.retry')}</button>
		</div>
	{:else if mode === 'no_games'}
		<div class="empty">
			<span>{t('home.no_games')}</span>
			<a href="/games" class="btn"><Icon name="list" />{t('home.no_games_add')}</a>
		</div>
	{:else if o && kpi}
		{#if mode === 'paused'}
			<div class="empty warn" role="status">
				<span>{tn('home.paused_all', o.status.accounts_total)}</span>
				<a href="/settings#accounts" class="btn">{t('settings.accounts')}</a>
			</div>
		{:else if mode === 'signed_out'}
			<div class="empty warn" role="status">
				<span>{tn('home.signed_out_all', o.status.accounts_total)}</span>
				<a href="/settings#accounts" class="btn">{t('accounts.log_in_again')}</a>
			</div>
		{/if}
		<div class="kpis" class:four={!!force}>
			<div class="kpi">
				<div class="label">{t('home.kpi_minutes')}</div>
				<div class="val">{formatInt(kpi.minutes_total)}<small>{t('home.kpi_min_unit')}</small></div>
				<div class="sub">{t('home.kpi_today', { n: formatInt(kpi.minutes_today) })}</div>
			</div>
			<a class="kpi" href="/inventory">
				<div class="label">{t('home.kpi_claimed')}</div>
				<div class="val">{formatInt(kpi.claimed_all_time)}</div>
				<div class="sub">{t('home.kpi_today', { n: formatInt(kpi.claimed_today) })}</div>
			</a>
			{#if force}
				<a class="kpi" href="/games">
					<div class="label">{t('home.kpi_force')}</div>
					{#if force.live.length > 0}
						<ul class="tile">
							{#each force.live as r (`${r.platform}/${r.channel}`)}
								<li>
									<span class="dot"></span><PlatformMark platform={r.platform} size="sm" /><b>{r.channel}</b>
									{#if r.watchers.length > 0}
										<span class="on">{r.watchers.join(', ')}</span>
									{:else}
										<span class="off">{t('home.kpi_force_nobody')}</span>
									{/if}
								</li>
							{/each}
						</ul>
						{#if force.more > 0}<div class="sub more">{t('home.kpi_force_more', { n: formatInt(force.more) })}</div>{/if}
					{:else}
						<div class="val none-live">{t('home.kpi_force_none')}</div>
						<div class="sub">{tn('home.kpi_force_waiting', force.total)}</div>
					{/if}
				</a>
			{/if}
			<a class="kpi" class:bad={unhealthy} href="/settings#accounts">
				<div class="label">
					{#if unhealthy}<Icon name="alert" size={12} />{/if}
					{t('home.kpi_mining')}
				</div>
				<div class="val">
					{formatInt(o.status.accounts_mining)}<small>/ {formatInt(o.status.accounts_total)}</small>
				</div>
				<div class="sub">
					{#if !unhealthy}
						{tn('home.kpi_healthy_n', kpi.accounts_healthy)}
					{:else if o.problems.length > 0}
						{t('home.kpi_signed_out', { name: o.problems[0].account_name })}
					{:else}
						{tn('home.kpi_unhealthy', o.status.accounts_total - kpi.accounts_healthy)}
					{/if}
				</div>
			</a>
		</div>

		<div class="layout">
			<div class="cards" bind:this={cardsEl}>
				<div class="cards-h">
					<h2>{t('home.games_title')} <span class="cnt">{cards.length}</span></h2>
					<a href="/games" class="edit"><Icon name="list" />{t('home.edit_games')}</a>
				</div>
				{#if cards.length === 0}<p class="none">{t('home.no_games')}</p>{/if}
				{#each listed as card, i (card.game.id)}
					<GameCard
						{card}
						over={i >= shownCount}
						{lastScan}
						{nextScan}
						{now}
						onopen={(tg) => (drawer = tg)}
						readyAccounts={mode === 'nothing_live' ? o.status.accounts_total - (o.status.accounts_paused ?? 0) : null}
					/>
				{/each}
				{#if hiddenCount > 0}
					<button type="button" class="more-games" aria-expanded={expanded} onclick={() => (expanded = !expanded)}
						>{expanded ? t('home.show_fewer') : tn('home.more_games', hiddenCount)}<Icon
							name={expanded ? 'chevron-up' : 'chevron-down'}
						/></button
					>
				{/if}
				{#if idle.length > 0}
					<ul class="not-mining" aria-label={t('home.not_mining')}>
						{#each idle as a (a.id)}
							<li>
								<PlatformMark platform={a.platform} /><span class="who">{a.name}</span><i class="sd {a.state}"></i><span
									>{t(IDLE_KEYS[a.state])}</span
								>
							</li>
						{/each}
					</ul>
				{/if}
			</div>

			<aside bind:this={asideEl}>
				<UpNext onopen={(tg) => (drawer = tg)} />
				<section class="sec" aria-label={t('home.claimed')}>
					<div class="sec-h">
						<h2>
							<svg viewBox="0 0 16 16" fill="none" stroke="var(--green)" stroke-width="1.6" aria-hidden="true"
								><circle cx="8" cy="8" r="6" /><path d="m5.3 8.2 1.8 1.8 3.6-3.8" stroke-linecap="round" stroke-linejoin="round" /></svg
							>{t('home.claimed')}
						</h2>
						<span class="meta">{t('home.claimed_meta', { n: formatInt(kpi.claimed_today) })}</span>
					</div>
					{#if live.recent.length === 0}
						<p class="none">{t('home.claimed_none')}</p>
					{:else}
						<ul class="claims">
							{#each sections as s (s.key)}
								{#if s.items.length > 0}
									<li class="grp">{s.label}</li>
									{#each s.items as c (`${c.account_id}:${c.benefit_id}`)}
										<li class="cl" class:old={s.key !== 'today'}>
											<svg viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true"
												><path d="m2.5 6.3 2.3 2.3L9.5 3.8" stroke-linecap="round" stroke-linejoin="round" /></svg
											>
											<span class="nm">{c.benefit_name}<span>{c.account_name}</span></span>
											<span class="tm">{when(c.claimed_at, s.key)}</span>
										</li>
									{/each}
								{/if}
							{/each}
						</ul>
					{/if}
					<div class="claims-foot">
						<span>{t('home.all_time', { n: formatInt(kpi.claimed_all_time) })}</span>
						<a href="/inventory" class="link">{t('home.all_rewards')}</a>
					</div>
				</section>
			</aside>
		</div>
	{/if}
</PageHead>

<CampaignDrawer target={drawer} onclose={() => (drawer = null)} />

<style>
	.kpis {
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		border: 1px solid var(--border-soft);
		border-radius: var(--radius);
		background: var(--surface);
		margin-bottom: 28px;
	}
	.kpi {
		padding: 16px 20px 18px;
		display: block;
		position: relative;
		min-width: 0;
		color: inherit;
		text-decoration: none;
	}
	.kpi + .kpi {
		border-left: 1px solid var(--border-soft);
	}
	.kpi .label {
		font-size: 12px;
		color: var(--t2);
		display: flex;
		align-items: center;
		gap: 6px;
	}
	.kpi .val {
		font-size: 30px;
		font-weight: 600;
		letter-spacing: -0.03em;
		line-height: 1.15;
		margin-top: 6px;
		font-variant-numeric: tabular-nums;
	}
	.kpi .val small {
		font-size: 14px;
		font-weight: 500;
		color: var(--t2);
		letter-spacing: 0;
		margin-left: 2px;
	}
	.kpi .sub {
		font-size: 12px;
		color: var(--t3);
		margin-top: 2px;
	}
	a.kpi:hover .label {
		color: var(--text);
	}
	a.kpi:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.kpis.four {
		grid-template-columns: 1fr 1fr 1.3fr 1fr;
	}
	.kpi .val.none-live {
		font-size: 20px;
		color: var(--t2);
	}
	.tile {
		list-style: none;
		margin: 8px 0 0;
		padding: 0;
		display: grid;
		gap: 6px;
	}
	.tile li {
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: 13px;
		min-width: 0;
	}
	.tile b {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		min-width: 0;
	}
	.tile li > :last-child {
		margin-left: auto;
		font-size: 12px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		min-width: 0;
	}
	.tile .dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--red);
		flex: none;
	}
	.tile .on {
		color: var(--green);
		font-weight: 500;
	}
	.tile .off {
		color: var(--t3);
	}
	.kpi .sub.more {
		margin-top: 6px;
	}
	.kpi.bad {
		background: var(--amber-soft);
		border-radius: 0 var(--radius) var(--radius) 0;
		box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--amber) 45%, transparent);
	}
	.kpi.bad .label,
	.kpi.bad .val {
		color: var(--amber);
	}
	.kpi.bad .val small {
		color: var(--amber);
		opacity: 0.8;
	}
	.kpi.bad .sub {
		color: var(--text);
	}
	.layout {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 320px;
		gap: 24px;
		align-items: start;
	}
	.cards {
		position: relative;
	}
	.cards-h {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		margin-bottom: 10px;
		min-height: 28px;
	}
	.cards-h h2 {
		margin: 0;
		font-size: 13px;
		font-weight: 600;
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.cnt {
		font: 400 12px var(--mono);
		color: var(--t2);
		background: var(--surface-3);
		border-radius: 10px;
		padding: 0 6px;
	}
	.not-mining {
		list-style: none;
		margin: 10px 0 0;
		padding: 0;
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.not-mining li {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		min-width: 0;
		max-width: 100%;
		padding: 3px 9px 3px 5px;
		border: 1px solid var(--border-soft);
		border-radius: 999px;
		background: var(--surface);
		font-size: 12px;
		color: var(--t3);
	}
	.not-mining .who {
		color: var(--t2);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		min-width: 0;
	}
	.sd {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		flex: none;
		background: var(--t3);
	}
	.sd.signed_out {
		background: var(--amber);
	}
	.sd.paused {
		background: transparent;
		box-shadow: inset 0 0 0 1.5px var(--t3);
	}
	.more-games {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 6px;
		width: 100%;
		margin-top: 12px;
		padding: 7px 12px;
		border: 1px dashed var(--border);
		border-radius: 10px;
		background: none;
		font: inherit;
		font-size: 12.5px;
		color: var(--t2);
		cursor: pointer;
	}
	.more-games:hover {
		color: var(--text);
		border-color: var(--t3);
	}
	.edit {
		color: var(--t2);
		font-size: 12.5px;
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 4px 10px;
		border: 1px solid var(--border);
		border-radius: 6px;
		text-decoration: none;
	}
	.edit:hover {
		color: var(--text);
		border-color: var(--t2);
	}
	.sec {
		margin-top: 28px;
		min-width: 0;
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
	.meta {
		font-size: 12px;
		color: var(--t3);
	}
	.empty {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		flex-wrap: wrap;
		padding: 14px 16px;
		border: 1px solid var(--border-soft);
		border-radius: var(--radius);
		background: var(--surface);
		font-size: 13px;
		margin-bottom: 20px;
	}
	.empty .btn {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font-size: 12.5px;
		font-weight: 500;
		background: var(--surface-2);
		border: 1px solid var(--border);
		border-radius: 6px;
		padding: 4px 10px;
		color: var(--text);
		text-decoration: none;
		white-space: nowrap;
	}
	.empty .btn:hover:not([disabled]) {
		border-color: var(--t3);
		background: var(--surface-3);
	}
	.empty .btn[disabled] {
		opacity: 0.45;
		cursor: default;
	}
	.empty .btn:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.empty.err {
		color: var(--red);
	}
	.empty.warn {
		background: var(--amber-soft);
		border-color: color-mix(in srgb, var(--amber) 45%, transparent);
	}
	.none {
		margin: 0;
		font-size: 13px;
		color: var(--t2);
	}
	.claims {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.grp {
		font-size: 12px;
		color: var(--t3);
		text-transform: uppercase;
		letter-spacing: 0.06em;
		font-weight: 600;
		padding: 10px 0 4px;
	}
	.grp:first-child {
		padding-top: 0;
	}
	.cl {
		display: grid;
		grid-template-columns: 14px minmax(0, 1fr) auto;
		gap: 10px;
		align-items: baseline;
		padding: 5px 0;
		border-bottom: 1px solid var(--border-soft);
	}
	.cl svg {
		width: 12px;
		height: 12px;
		color: var(--green);
		position: relative;
		top: 1px;
	}
	.old svg {
		color: var(--t3);
	}
	.nm {
		font-size: 13px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.nm span {
		color: var(--t3);
		font-size: 12px;
		margin-left: 6px;
	}
	.tm {
		font: 12px var(--mono);
		color: var(--t3);
		white-space: nowrap;
	}
	.claims-foot {
		display: flex;
		justify-content: space-between;
		padding-top: 10px;
		font-size: 12px;
		color: var(--t3);
	}
	.link {
		color: var(--t2);
		font-size: 12px;
	}
	.link:hover {
		color: var(--accent);
	}
	@media (max-width: 960px) {
		.layout {
			grid-template-columns: minmax(0, 1fr);
		}
	}
	@media (max-width: 720px) {
		.kpis,
		.kpis.four {
			grid-template-columns: 1fr 1fr;
		}
		.kpi {
			padding: 14px 16px;
		}
		.kpi .val {
			font-size: 24px;
		}
		.kpis:not(.four) .kpi:nth-child(3) {
			grid-column: 1 / -1;
			border-left: 0;
			border-top: 1px solid var(--border-soft);
		}
		.kpis.four .kpi:nth-child(3),
		.kpis.four .kpi:nth-child(4) {
			border-top: 1px solid var(--border-soft);
		}
		.kpis.four .kpi:nth-child(3) {
			border-left: 0;
		}
		.kpi.bad {
			border-radius: 0 0 var(--radius) var(--radius);
		}
		.kpis.four .kpi.bad {
			border-radius: 0 0 var(--radius) 0;
		}
	}
</style>
