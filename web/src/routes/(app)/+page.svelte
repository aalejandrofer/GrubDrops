<script lang="ts">
	import FirstRun from '$lib/components/FirstRun.svelte';
	import GameCard from '$lib/components/GameCard.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import PageHead from '$lib/components/PageHead.svelte';
	import UpNext from '$lib/components/UpNext.svelte';
	import { clock, formatInt, relTime, shortDate } from '$lib/format';
	import { gameCards, groupClaims, homeView } from '$lib/home';
	import { t, tn } from '$lib/i18n/index.svelte';
	import { getLive, getNet } from '$lib/live-context';
	import { prefs } from '$lib/prefs.svelte';

	const live = getLive();
	const net = getNet();
	let retrying = $state(false);

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
			unreachable: live.unreachable || net.down
		})
	);
	const dashboard = $derived(
		mode === 'healthy' || mode === 'nothing_live' || mode === 'signed_out' || mode === 'paused'
	);
	const lastScan = $derived(live.games?.last_scan_at ?? o?.status.last_scan_at ?? null);
	const nextScan = $derived(live.games?.next_scan_at ?? o?.status.next_scan_at ?? null);
	const pausedIds = $derived(new Set(live.accounts.filter((a) => !a.enabled).map((a) => a.id)));
	const cards = $derived(gameCards(live.games?.games ?? [], live.runners, pausedIds));
	const groups = $derived(groupClaims(live.recent, now, prefs.zone));
	const stamp = $derived(
		live.updatedAt === null
			? undefined
			: t('home.updated', { rel: relTime(new Date(live.updatedAt).toISOString(), now) })
	);
	const kpi = $derived(o?.kpis);
	const unhealthy = $derived(o && kpi ? kpi.accounts_healthy < o.status.accounts_total : false);
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
				<button type="button" class="btn" disabled={retrying} onclick={retry}>{t('rewards.retry')}</button>
			</div>
		{/if}
	{:else if mode === 'loading'}
		<p class="none">{t('home.loading')}</p>
	{:else if mode === 'error'}
		<div class="empty err" role="alert">
			<span>{t('home.load_failed')}</span>
			<button type="button" class="btn" disabled={retrying} onclick={retry}>{t('rewards.retry')}</button>
		</div>
	{:else if mode === 'first_run'}
		<FirstRun />
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
		<div class="kpis">
			<div class="kpi">
				<div class="label">{t('home.kpi_minutes')}</div>
				<div class="val">{formatInt(kpi.minutes_total)}<small>{t('home.kpi_min_unit')}</small></div>
				<div class="sub">
					{tn('home.kpi_minutes_sub', o.status.accounts_total, { today: formatInt(kpi.minutes_today) })}
				</div>
			</div>
			<a class="kpi" href="/rewards">
				<div class="label">{t('home.kpi_claimed')}</div>
				<div class="val">{formatInt(kpi.claimed_today)}</div>
				<div class="sub">{t('home.all_time', { n: formatInt(kpi.claimed_all_time) })}</div>
			</a>
			<a class="kpi" class:bad={unhealthy} href="/settings#accounts">
				<div class="label">
					{#if unhealthy}<Icon name="alert" size={12} />{/if}
					{t('home.kpi_healthy')}
				</div>
				<div class="val">
					{formatInt(kpi.accounts_healthy)}<small>/ {formatInt(o.status.accounts_total)}</small>
				</div>
				<div class="sub">
					{#if !unhealthy}
						{t('home.kpi_all_signed_in')}
					{:else if o.problems.length > 0}
						{t('home.kpi_signed_out', { name: o.problems[0].account_name })}
					{:else}
						{tn('home.kpi_unhealthy', o.status.accounts_total - kpi.accounts_healthy)}
					{/if}
				</div>
			</a>
		</div>

		<div class="layout">
			<div class="cards">
				<div class="cards-h">
					<h2>{t('home.games_title')} <span class="cnt">{cards.length}</span></h2>
					<a href="/games" class="edit"><Icon name="list" />{t('home.edit_games')}</a>
				</div>
				{#each cards as card (card.game.id)}
					<GameCard
						{card}
						{lastScan}
						{nextScan}
						{now}
						readyAccounts={mode === 'nothing_live' ? o.status.accounts_total - (o.status.accounts_paused ?? 0) : null}
					/>
				{/each}
			</div>

			<aside>
				<UpNext />
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
						<a href="/rewards" class="link">{t('home.all_rewards')}</a>
					</div>
				</section>
			</aside>
		</div>
	{/if}
</PageHead>

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
		.kpis {
			grid-template-columns: 1fr 1fr;
		}
		.kpi {
			padding: 14px 16px;
		}
		.kpi .val {
			font-size: 24px;
		}
		.kpi:nth-child(3) {
			grid-column: 1 / -1;
			border-left: 0;
			border-top: 1px solid var(--border-soft);
		}
		.kpi.bad {
			border-radius: 0 0 var(--radius) var(--radius);
		}
	}
</style>
