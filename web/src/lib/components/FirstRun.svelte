<script lang="ts">
	import { onDestroy } from 'svelte';
	import { api } from '$lib/app';
	import type { SearchHit } from '$lib/api/types';
	import { tn, t } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';
	import { normName } from '$lib/home';
	import PlatformMark from './PlatformMark.svelte';

	const live = getLive();
	let q = $state('');
	let hits = $state<SearchHit[]>([]);
	let failed = $state(false);
	let searchFailed = $state(false);
	let busy = $state<string | null>(null);
	let timer: ReturnType<typeof setTimeout> | undefined;
	let seq = 0;

	const accounts = $derived(live.overview?.status.accounts_total ?? 0);
	const gameCount = $derived(live.games?.games.length ?? 0);
	const step1 = $derived(accounts > 0);
	const step2 = $derived(gameCount > 0);
	const query = $derived(q.trim());
	const exact = $derived(hits.some((h) => normName(h.name) === normName(query)));

	function onInput(): void {
		clearTimeout(timer);
		failed = false;
		searchFailed = false;
		const text = q.trim();
		const mine = ++seq;
		if (!text) {
			hits = [];
			return;
		}
		timer = setTimeout(async () => {
			try {
				const r = await api.searchGames(text);
				if (mine === seq) hits = r;
			} catch {
				if (mine === seq) {
					hits = [];
					searchFailed = true;
				}
			}
		}, 150);
	}

	async function add(name: string): Promise<void> {
		busy = name;
		failed = false;
		try {
			await api.addGame(name);
			q = '';
			hits = [];
			seq++;
			try {
				live.games = await api.games();
			} catch {
				/* the games event refetches */
			}
		} catch {
			failed = true;
		} finally {
			busy = null;
		}
	}

	onDestroy(() => clearTimeout(timer));
</script>

<div class="layout">
	<section class="setup" aria-label={t('home.setup_title')}>
		<h2>{t('home.setup_title')}</h2>
		<p>{t('home.setup_lead')}</p>
		<ol class="steps">
			<li class:cur={!step1} class:done={step1}>
				<span class="sn"
					><b>1</b><svg viewBox="0 0 12 12" aria-hidden="true"><path d="m2.5 6.3 2.3 2.3L9.5 3.8" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg></span
				>
				<div>
					<h3>{t('home.step1_title')}</h3>
					<p>{t('home.step1_body')}</p>
					<div class="acts">
						<a href="/settings?add=twitch#accounts" class="btn"><PlatformMark platform="twitch" />{t('home.add_twitch')}</a>
						<a href="/settings?add=kick#accounts" class="btn"><PlatformMark platform="kick" />{t('home.add_kick')}</a>
					</div>
				</div>
			</li>
			<li class:cur={step1 && !step2} class:done={step2}>
				<span class="sn"
					><b>2</b><svg viewBox="0 0 12 12" aria-hidden="true"><path d="m2.5 6.3 2.3 2.3L9.5 3.8" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg></span
				>
				<div>
					<h3>
						{t('home.step2_title')}
						{#if step2}<small>{tn('home.games_added', gameCount)}</small>{/if}
					</h3>
					<p>{t('home.step2_body')}</p>
					{#if !step2}
						<p class="no-games">{t('home.no_games')} <a href="/games">{t('home.no_games_add')}</a></p>
					{/if}
					<div class="pick">
						<label class="search">
							<svg class="i" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg>
							<input
								type="search"
								placeholder={t('home.search_ph')}
								autocomplete="off"
								aria-label={t('home.search_label')}
								bind:value={q}
								oninput={onInput}
							/>
						</label>
						{#if query && (hits.length > 0 || !exact)}
							<ul class="sugg">
								{#each hits as h (h.id)}
									<li>
										<span class="thumb" aria-hidden="true">{Array.from(h.name)[0]?.toUpperCase() ?? '?'}</span>
										<span class="nm"><b>{h.name}</b></span>
										<button type="button" class="add-btn" disabled={h.chosen || busy !== null} onclick={() => add(h.name)}>
											{h.chosen ? t('home.added') : t('home.add')}
										</button>
									</li>
								{/each}
								{#if !exact}
									<li>
										<span class="thumb" aria-hidden="true">+</span>
										<span class="nm"><b>{t('home.add_named', { q: query })}</b></span>
										<button type="button" class="add-btn" disabled={busy !== null} onclick={() => add(query)}>{t('home.add')}</button>
									</li>
								{/if}
							</ul>
						{/if}
						{#if failed}<p class="err" role="alert">{t('home.add_failed')}</p>{/if}
						{#if searchFailed}<p class="err" role="alert">{t('home.search_failed')}</p>{/if}
					</div>
				</div>
			</li>
			<li>
				<span class="sn"><b>3</b></span>
				<div>
					<h3>{t('home.step3_title')}</h3>
					<p>{t('home.step3_body')}</p>
				</div>
			</li>
		</ol>
	</section>

	<aside>
		<section class="panel">
			<div class="panel-h"><h2>{t('home.how_title')}</h2></div>
			<ul class="how">
				<li><span>1</span><div><b>{t('home.how1_h')}</b> {t('home.how1_b')}</div></li>
				<li><span>2</span><div><b>{t('home.how2_h')}</b> {t('home.how2_b')}</div></li>
				<li><span>3</span><div><b>{t('home.how3_h')}</b> {t('home.how3_b')}</div></li>
			</ul>
		</section>
	</aside>
</div>

<style>
	.layout {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 320px;
		gap: 24px;
		align-items: start;
	}
	@media (max-width: 960px) {
		.layout {
			grid-template-columns: minmax(0, 1fr);
		}
	}
	.setup {
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		padding: 22px 24px 8px;
	}
	.setup > h2 {
		margin: 0;
		font-size: 15px;
		font-weight: 600;
		letter-spacing: -0.01em;
	}
	.setup > p {
		margin: 2px 0 0;
		color: var(--t2);
		font-size: 13px;
	}
	.steps {
		list-style: none;
		margin: 18px 0 0;
		padding: 0;
	}
	.steps > li {
		display: grid;
		grid-template-columns: 28px minmax(0, 1fr);
		gap: 14px;
		padding: 16px 0 18px;
		border-top: 1px solid var(--border-soft);
	}
	.sn {
		width: 24px;
		height: 24px;
		border-radius: 50%;
		display: grid;
		place-items: center;
		font: 500 12px var(--mono);
		color: var(--t2);
		border: 1px solid var(--border);
	}
	.sn svg {
		width: 12px;
		height: 12px;
		display: none;
	}
	.cur .sn {
		color: var(--accent);
		border-color: var(--accent);
		background: var(--accent-soft);
	}
	.done .sn {
		color: var(--green);
		border-color: color-mix(in srgb, var(--green) 50%, transparent);
		background: var(--green-soft);
	}
	.done .sn b {
		display: none;
	}
	.done .sn svg {
		display: block;
	}
	h3 {
		margin: 1px 0 0;
		font-size: 14px;
		font-weight: 600;
		display: flex;
		align-items: baseline;
		gap: 8px;
	}
	h3 small {
		font: 12px var(--mono);
		color: var(--green);
		font-weight: 400;
	}
	.steps p {
		margin: 2px 0 0;
		color: var(--t2);
		font-size: 13px;
	}
	.acts {
		display: flex;
		gap: 8px;
		flex-wrap: wrap;
		margin-top: 12px;
	}
	.no-games a {
		color: var(--accent);
	}
	.btn {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		font-size: 13px;
		font-weight: 500;
		padding: 6px 12px;
		border-radius: 7px;
		border: 1px solid var(--border);
		background: var(--surface-2);
		color: var(--text);
		white-space: nowrap;
		text-decoration: none;
	}
	.btn:hover {
		border-color: var(--t2);
	}
	.btn:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.pick {
		margin-top: 12px;
		max-width: 520px;
	}
	.search {
		display: flex;
		align-items: center;
		gap: 10px;
		height: 40px;
		padding: 0 14px;
		border: 1px solid var(--border);
		border-radius: 10px;
		background: var(--bg);
		color: var(--t3);
	}
	.search:focus-within {
		border-color: var(--accent);
		box-shadow: 0 0 0 3px var(--accent-soft);
	}
	.search input {
		flex: 1;
		min-width: 0;
		background: none;
		border: 0;
		outline: 0;
		color: var(--text);
		font: inherit;
	}
	.search input::placeholder {
		color: var(--t3);
	}
	.sugg {
		margin: 6px 0 0;
		padding: 4px;
		list-style: none;
		border: 1px solid var(--border);
		border-radius: 10px;
		background: var(--surface-2);
	}
	.sugg li {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 8px 10px;
		border-radius: 6px;
	}
	.sugg li:hover {
		background: var(--surface-3);
	}
	.thumb {
		width: 28px;
		height: 37px;
		border-radius: 4px;
		flex: none;
		display: grid;
		place-items: center;
		background: var(--art-bg) repeating-linear-gradient(45deg, rgba(255, 255, 255, 0.05) 0 2px, transparent 2px 8px);
		color: var(--t2);
		font: 600 12px var(--font);
	}
	.nm {
		flex: 1;
		min-width: 0;
	}
	.nm b {
		font-weight: 500;
		display: block;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.add-btn {
		font: inherit;
		font-size: 12.5px;
		font-weight: 500;
		padding: 4px 10px;
		border-radius: 6px;
		border: 1px solid var(--border);
		background: var(--surface);
		color: var(--text);
		cursor: pointer;
	}
	.add-btn:hover:not(:disabled) {
		border-color: var(--accent);
		color: var(--accent);
	}
	.add-btn:disabled {
		color: var(--t2);
		background: none;
		border-color: transparent;
		cursor: default;
	}
	.err {
		color: var(--red);
		font-size: 12px;
	}
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
	.panel-h h2 {
		margin: 0;
		font-size: 13px;
		font-weight: 600;
	}
	.how {
		padding: 0 14px 6px;
		margin: 8px 0 0;
		list-style: none;
		display: grid;
	}
	.how li {
		display: grid;
		grid-template-columns: 18px minmax(0, 1fr);
		gap: 8px;
		padding: 9px 0;
		font-size: 13px;
		color: var(--t2);
	}
	.how li + li {
		border-top: 1px solid var(--border-soft);
	}
	.how li span {
		font: 12px var(--mono);
		color: var(--t3);
		padding-top: 1px;
	}
	.how li b {
		color: var(--text);
		font-weight: 500;
	}
	@media (max-width: 720px) {
		.setup {
			padding: 18px 16px 4px;
		}
	}
</style>
