<script lang="ts">
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { t, tn } from '$lib/i18n/index.svelte';
	import Shell from './_Shell.svelte';
	import { fake, type Step } from './_variants';

	let { step }: { step: Step } = $props();
	const uid = $props.id();
	const run = fake.runs[1];
	const games = fake.games.filter((g) => g.platform === 'kick');
	const mining = $derived(step === 3);
</script>

<Shell>
	<div class="head">
		<h1>{t('nav.home')}</h1>
		{#if mining}
			<span class="live">{tn('status.mining', 1, { m: 1 })}</span>
		{:else}
			<span class="live grey">{t('status.not_set_up')}</span>
		{/if}
	</div>

	<div class="kpis">
		<div class="kpi panel">
			<div class="lbl">{t('home.kpi_minutes')}</div>
			<div class="val mono">{mining ? run.m : '–'}{#if mining}<small>{t('home.kpi_min_unit')}</small>{/if}</div>
		</div>
		<div class="kpi panel">
			<div class="lbl">{t('home.kpi_claimed')}</div>
			<div class="val mono">{mining ? 0 : '–'}</div>
		</div>
		<div class="kpi panel">
			<div class="lbl">{t('home.kpi_healthy')}</div>
			<div class="val mono">{step === 1 ? '–' : '1'}{#if step > 1}<small>/ 1</small>{/if}</div>
		</div>
	</div>

	<div class="cols">
		<section class="panel sec" class:active={step === 2} aria-labelledby={`${uid}-g`}>
			<div class="sec-h">
				<h2 id={`${uid}-g`}>{t('home.games_title')}</h2>
				{#if mining}<span class="cnt mono">{games.length}</span>{/if}
			</div>
			{#if step === 1}
				<div class="empty">
					<p class="faint">Add an account first.</p>
					<label class="search off">
						<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg>
						<input type="search" disabled aria-label={t('home.search_label')} placeholder={t('home.search_ph')} />
					</label>
				</div>
			{:else if step === 2}
				<div class="empty">
					<p>{t('home.no_games')} {t('home.step2_body')}</p>
					<label class="search">
						<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg>
						<input type="search" value="Apex" aria-label={t('home.search_label')} placeholder={t('home.search_ph')} />
					</label>
					<ul class="sugg">
						<li><span class="art" aria-hidden="true">A</span><b class="grow">Apex Legends</b><span class="faint">Kick, Twitch</span><button type="button" class="btn">{t('home.add')}</button></li>
						<li><span class="art" aria-hidden="true">+</span><b class="grow">{t('home.add_named', { q: 'Apex' })}</b><button type="button" class="btn">{t('home.add')}</button></li>
					</ul>
				</div>
			{:else}
				{#each games as g, i (g.name)}
					<div class="game">
						<span class="art lg" aria-hidden="true">{g.name[0]}</span>
						<div class="grow">
							<div class="g-h"><span class="mono rank">{i + 1}</span><b>{g.name}</b><span class="chip green">{t('accounts.status.mining')}</span></div>
							<p class="faint meta"><PlatformMark platform={g.platform} size="sm" />{g.campaign} · {g.drops} drops · ends {g.ends}</p>
							<div class="acc">
								<PlatformMark platform={run.platform} />
								<span class={`pf-name ${run.platform}`}>{run.account}</span>
								<span class="muted grow">{run.drop} on {run.channel}</span>
								<span class="mono faint">{run.m} / {run.req} min</span>
							</div>
							<div class="bar" aria-hidden="true"><i style={`width:${(run.m / run.req) * 100}%`}></i></div>
						</div>
					</div>
				{/each}
				<a class="more" href="/games">Add more on Games</a>
			{/if}
		</section>

		<section class="panel sec" class:active={step === 1} aria-labelledby={`${uid}-a`}>
			<div class="sec-h"><h2 id={`${uid}-a`}>{t('settings.accounts')}</h2></div>
			{#if step === 1}
				<div class="empty">
					<p>No accounts.</p>
					<div class="row-b">
						<button type="button" class="btn"><PlatformMark platform="twitch" />{t('home.add_twitch')}</button>
						<button type="button" class="btn on"><PlatformMark platform="kick" />{t('home.add_kick')}</button>
					</div>
					<div class="field ck">
						<label for={`${uid}-ck`}>{t('accounts.cookies_label')}</label>
						<textarea id={`${uid}-ck`} rows="4" placeholder="# Netscape HTTP Cookie File&#10;.kick.com	TRUE	/	TRUE	…"></textarea>
						<small class="faint">{t('accounts.cookies_help')}</small>
						<div class="row-b">
							<button type="button" class="btn">{t('accounts.cookies_file')}</button>
							<span class="grow"></span>
							<button type="button" class="btn primary" disabled>{t('accounts.save')}</button>
						</div>
					</div>
				</div>
			{:else}
				<div class="arow">
					<PlatformMark platform="kick" />
					<span class="pf-name kick nm">{fake.kick}</span>
					<span class="grow"></span>
					{#if mining}
						<span class="chip green">{t('accounts.status.mining')}</span>
					{:else}
						<span class="chip">{t('accounts.state.no_games')}</span>
					{/if}
				</div>
				<a class="more" href="/settings#accounts">{t('accounts.add')}</a>
			{/if}
		</section>
	</div>
</Shell>

<style>
	.head {
		display: flex;
		align-items: center;
		gap: 16px;
		margin-bottom: 24px;
	}
	.kpis {
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		gap: 12px;
	}
	.kpi {
		padding: 12px 16px 14px;
	}
	.val {
		margin-top: 2px;
		font-size: 22px;
		font-weight: 600;
	}
	.val small {
		margin-left: 4px;
		font-size: 13px;
		font-weight: 400;
		color: var(--t3);
	}
	.cols {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 340px;
		gap: 16px;
		margin-top: 16px;
		align-items: start;
	}
	.sec {
		padding: 14px 16px 16px;
	}
	.sec.active {
		border-color: color-mix(in srgb, var(--accent) 55%, var(--border-soft));
	}
	.sec-h {
		display: flex;
		align-items: center;
		gap: 8px;
		margin-bottom: 10px;
	}
	h2 {
		margin: 0;
		font-size: 13px;
		font-weight: 600;
	}
	.cnt {
		font-size: 12px;
		color: var(--t3);
	}
	.empty p {
		margin: 0 0 12px;
		font-size: 13px;
		max-width: 62ch;
	}
	.search.off {
		border-color: var(--border-soft);
		box-shadow: none;
		opacity: 0.6;
	}
	.sugg b {
		font-weight: 500;
	}
	.sugg .faint {
		font-size: 12px;
	}
	.row-b {
		display: flex;
		gap: 8px;
		flex-wrap: wrap;
	}
	.row-b .on {
		border-color: var(--accent);
		background: var(--accent-soft);
	}
	.ck {
		margin-top: 14px;
	}
	.ck small {
		font-size: 12px;
		margin-bottom: 6px;
	}
	.game {
		display: flex;
		gap: 14px;
	}
	.art.lg {
		width: 52px;
		height: 69px;
		border-radius: 6px;
		font-size: 18px;
	}
	.g-h {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.rank {
		font-size: 12px;
		color: var(--accent);
		font-weight: 600;
	}
	.g-h .chip {
		margin-left: auto;
	}
	.meta {
		display: flex;
		align-items: center;
		gap: 6px;
		margin: 2px 0 10px;
		font-size: 12px;
	}
	.acc {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 13px;
		margin-bottom: 6px;
	}
	.acc .mono {
		font-size: 12px;
	}
	.nm {
		font-size: 14px;
		font-weight: 600;
	}
	.arow {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 4px 0 12px;
		border-bottom: 1px solid var(--border-soft);
	}
	.more {
		display: inline-block;
		margin-top: 12px;
		font-size: 13px;
		color: var(--accent) !important;
	}
	@container (max-width: 820px) {
		.cols {
			grid-template-columns: minmax(0, 1fr);
		}
	}
	@container (max-width: 520px) {
		.kpis {
			grid-template-columns: minmax(0, 1fr);
		}
	}
</style>
