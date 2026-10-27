<script lang="ts">
	import { onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import logo from '../../../../../assets/img/logo.png';
	import { api } from '$lib/app';
	import type { Game, SearchHit } from '$lib/api/types';
	import AddAccount from '$lib/components/AddAccount.svelte';
	import BoxArt from '$lib/components/BoxArt.svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { commitGames, dupePriority, moveGame, suggestions, withoutId } from '$lib/games';
	import { t, tn } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';
	import { canContinue, segments, signedIn, startStep, type Step } from '$lib/onboarding';

	const live = getLive();

	let step = $state<Step | null>(null);
	let added = $state(false);

	// Open on the first unfinished step once data loads; a finished setup goes Home.
	$effect(() => {
		if (step !== null || !live.overview || !live.games) return;
		// Accounts exist but the list has not loaded yet.
		if (live.overview.status.accounts_total > 0 && live.accounts.length === 0) return;
		const s = startStep(signedIn(live.accounts).length, live.games.games.length);
		if (s === null) void goto('/', { replaceState: true });
		else step = s;
	});

	const ready = $derived(signedIn(live.accounts));
	const games = $derived(live.games?.games ?? []);
	const open = $derived(
		step !== null && canContinue(step, { signedIn: added ? Math.max(1, ready.length) : ready.length, games: games.length })
	);
	const titles = $derived([t('welcome.account_title'), t('welcome.games_title')]);

	// Step 1
	let addError = $state('');
	// Fixed when the step opens, so an account that signs in mid-flow does not unmount the flow.
	let showFlow = $state(true);

	let removing = $state<string | null>(null);
	let removeFailed = $state(false);

	async function onAdded(): Promise<void> {
		added = true;
		showFlow = false;
		await live.refetchAll().catch(() => {});
	}

	async function removeAccount(id: string): Promise<void> {
		removing = id;
		removeFailed = false;
		try {
			await api.removeAccount(id);
		} catch {
			removeFailed = true;
		}
		removing = null;
		await live.refetchAll().catch(() => {});
	}

	// Step 2
	let q = $state('');
	let hits = $state<SearchHit[]>([]);
	let searchFailed = $state(false);
	let addFailed = $state(false);
	let saveFailed = $state(false);
	let dupe = $state<number | null>(null);
	let busy = $state<string | null>(null);
	let saving = $state(false);
	let timer: ReturnType<typeof setTimeout> | undefined;
	let seq = 0;
	const sugg = $derived(suggestions(hits, q));

	function onInput(): void {
		clearTimeout(timer);
		searchFailed = false;
		dupe = null;
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
		}, 200);
	}

	async function addGame(name: string): Promise<void> {
		busy = name;
		addFailed = false;
		dupe = null;
		try {
			await api.addGame(name);
			q = '';
			hits = [];
			seq++;
		} catch (e) {
			dupe = dupePriority(e);
			addFailed = dupe === null;
		} finally {
			busy = null;
		}
		try {
			live.games = await api.games();
		} catch {
			/* the games event refetches */
		}
	}

	async function commit(next: Game[]): Promise<void> {
		if (saving) return;
		saving = true;
		saveFailed = false;
		const ok = await commitGames(
			{
				get: () => live.games?.games ?? [],
				set: (g) => {
					if (live.games) live.games = { ...live.games, games: g.map((x, i) => ({ ...x, priority: i + 1 })) };
				},
				setGames: (ids) => api.setGames(ids),
				fetchGames: async () => (await api.games()).games
			},
			next
		);
		saving = false;
		saveFailed = !ok;
	}

	function removeGame(id: string): Promise<void> {
		const keep = withoutId(
			games.map((g) => g.id),
			id
		);
		return commit(keep.map((k) => games.find((g) => g.id === k)!));
	}

	function next(): void {
		if (!step || !open) return;
		if (step === 2) {
			// Scan now so mining does not wait for the next scheduled scan.
			api.scanGames().catch(() => {});
			void live.refetchAll().catch(() => {});
			void goto('/');
			return;
		}
		step = 2;
	}

	onDestroy(() => clearTimeout(timer));
</script>

<div class="ob">
	<header class="top">
		<span class="brand"><img src={logo} alt="" /><span>{t('app.name')}</span></span>
		<a class="quiet-link" href="/settings">{t('welcome.skip')}</a>
	</header>

	<main class="wiz">
		{#if step === null}
			{#if live.failed.overview || live.failed.games}
				<div class="empty" role="alert">
					<span>{t('home.load_failed')}</span>
					<button type="button" class="btn" onclick={() => live.refetchAll().catch(() => {})}>{t('inventory.retry')}</button>
				</div>
			{:else}
				<p class="faint">{t('home.loading')}</p>
			{/if}
		{:else}
			<div class="prog">
				<span class="mono faint">{t('welcome.step', { n: step, total: 2 })}</span>
				<ol class="segs" aria-label={t('welcome.progress')}>
					{#each segments(step) as s, i (i)}
						<li class={s} aria-current={s === 'cur' ? 'step' : undefined}><span class="sr">{titles[i]}</span></li>
					{/each}
				</ol>
			</div>

			<h1>{titles[step - 1]}</h1>

			{#if step === 1}
				<p class="lead">{t('welcome.account_lead')}</p>
				{#if ready.length > 0}
					<ul class="panel accts" aria-label={t('welcome.accounts_added')}>
						{#each ready as a (a.id)}
							<li>
								<PlatformMark platform={a.platform} />
								<b>{a.name}</b>
								<span class="chip green">{t('accounts.device_done')}</span>
								<button type="button" class="ico" aria-label={t('welcome.remove_account', { name: a.name })} disabled={removing !== null} onclick={() => removeAccount(a.id)}
									><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12" /></svg></button
								>
							</li>
						{/each}
					</ul>
					{#if removeFailed}<p class="bad" role="alert">{t('accounts.remove_failed')}</p>{/if}
				{/if}
				{#if addError}<p class="bad" role="alert">{addError}</p>{/if}
				{#if showFlow}
					<AddAccount onadded={onAdded} onfail={(msg) => (addError = msg)} onpick={() => (addError = '')} />
					{#if ready.length > 0}
						<button type="button" class="btn quiet more" onclick={() => ((showFlow = false), (addError = ''))}>{t('welcome.cancel_add')}</button>
					{/if}
				{:else}
					<button type="button" class="btn more" onclick={() => (showFlow = true)}>{t('welcome.add_another')}</button>
				{/if}
			{:else if step === 2}
				<p class="lead">{t('welcome.games_lead')}</p>
				<div class="pick">
					<label class="search">
						<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true"
							><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg
						>
						<!-- svelte-ignore a11y_autofocus -->
						<input
							type="search"
							autofocus
							autocomplete="off"
							placeholder={t('home.search_ph')}
							aria-label={t('home.search_label')}
							bind:value={q}
							oninput={onInput}
						/>
					</label>
					{#if sugg.length > 0}
						<ul class="sugg">
							{#each sugg as s (s.kind === 'hit' ? `h:${s.hit.id}` : 'add')}
								<li>
									{#if s.kind === 'hit'}
										<span class="art" aria-hidden="true">{Array.from(s.hit.name)[0]?.toUpperCase() ?? '?'}</span>
										<b class="grow">{s.hit.name}</b>
										<button type="button" class="btn sm" disabled={s.hit.chosen || busy !== null} onclick={() => addGame(s.hit.name)}>
											{s.hit.chosen ? t('games.added') : t('games.add')}
										</button>
									{:else}
										<span class="art" aria-hidden="true">+</span>
										<b class="grow">{t('games.add_named', { name: s.name })}</b>
										<button type="button" class="btn sm" disabled={busy !== null} onclick={() => addGame(s.name)}>{t('games.add')}</button>
									{/if}
								</li>
							{/each}
						</ul>
					{/if}
					{#if addFailed}<p class="bad" role="alert">{t('home.add_failed')}</p>{/if}
					{#if dupe !== null}<p class="note" role="status">{t('games.already', { n: dupe })}</p>{/if}
					{#if searchFailed}<p class="bad" role="alert">{t('home.search_failed')}</p>{/if}
				</div>

				<h2 class="list-h">{t('welcome.order')}</h2>
				{#if saveFailed}<p class="bad" role="alert">{t('games.save_failed')}</p>{/if}
				{#if games.length === 0}
					<p class="faint empty-list">{t('games.empty')}</p>
				{:else}
					<ol class="order panel">
						{#each games as g, i (g.id)}
							<li>
								<span class="mono rank">{i + 1}</span>
								<span class="thumb"><BoxArt src={g.box_art_url} name={g.name} size="sm" /></span>
								<div class="grow gm">
									<b>{g.name}</b>
									{#if g.campaigns.length > 0}
										<span class="faint meta">
											{#each [...new Set(g.campaigns.map((c) => c.platform))] as p (p)}<PlatformMark platform={p} size="sm" />{/each}
											{tn('welcome.campaigns', g.campaigns.length)}
										</span>
									{/if}
								</div>
								<span class="moves">
									<button type="button" class="ico" aria-label={t('welcome.move_up', { game: g.name })} disabled={i === 0 || saving} onclick={() => commit(moveGame(games, i, i - 1))}
										><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m6 15 6-6 6 6" /></svg></button
									>
									<button
										type="button"
										class="ico"
										aria-label={t('welcome.move_down', { game: g.name })}
										disabled={i === games.length - 1 || saving}
										onclick={() => commit(moveGame(games, i, i + 1))}
										><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg></button
									>
									<button type="button" class="ico" aria-label={t('welcome.remove', { game: g.name })} disabled={saving} onclick={() => removeGame(g.id)}
										><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12" /></svg></button
									>
								</span>
							</li>
						{/each}
					</ol>
				{/if}
			{/if}

			<div class="foot">
				{#if step === 2}<button type="button" class="btn quiet" onclick={() => {
							showFlow = ready.length === 0;
							step = 1;
						}}>{t('welcome.back')}</button>{/if}
				<span class="grow"></span>
				<button type="button" class="btn primary" disabled={!open} onclick={next}>{t(step === 1 ? 'welcome.continue' : 'welcome.start')}</button>
			</div>
		{/if}
	</main>
</div>

<style>
	.ob {
		min-height: 100vh;
		background: var(--bg);
		color: var(--text);
	}
	.ob :global(::selection) {
		background: var(--accent-soft);
		color: var(--text);
	}
	.top {
		height: 52px;
		max-width: 1160px;
		margin: 0 auto;
		padding: 0 24px;
		display: flex;
		align-items: center;
		justify-content: space-between;
		border-bottom: 1px solid var(--border-soft);
	}
	.brand {
		display: flex;
		align-items: center;
		gap: 8px;
		font-weight: 600;
		letter-spacing: -0.01em;
	}
	.brand img {
		width: 24px;
		height: 24px;
		border-radius: 6px;
	}
	.quiet-link {
		font-size: 13px;
		color: var(--t2);
		text-decoration: none;
		padding: 6px 8px;
		border-radius: 6px;
	}
	.quiet-link:hover {
		color: var(--text);
		background: var(--surface-2);
	}
	.wiz {
		max-width: 560px;
		margin: 0 auto;
		padding: 40px 24px 64px;
	}
	.prog {
		display: flex;
		align-items: center;
		gap: 14px;
		margin-bottom: 22px;
	}
	.prog .mono {
		font-size: 12px;
		white-space: nowrap;
	}
	.segs {
		flex: 1;
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		gap: 4px;
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.segs li {
		height: 4px;
		border-radius: 2px;
		background: var(--surface-3);
		transition: background 0.3s cubic-bezier(0.16, 1, 0.3, 1);
	}
	.segs li.cur {
		background: var(--accent);
	}
	.segs li.done {
		background: var(--green);
	}
	.sr {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		clip-path: inset(50%);
	}
	h1 {
		margin: 0;
		font-size: 22px;
		font-weight: 600;
		letter-spacing: -0.02em;
		text-wrap: balance;
	}
	.lead {
		margin: 4px 0 20px;
		font-size: 13.5px;
		color: var(--t2);
		max-width: 62ch;
	}
	.faint {
		color: var(--t3);
	}
	.mono {
		font-family: var(--mono);
		font-variant-numeric: tabular-nums;
	}
	.grow {
		flex: 1;
		min-width: 0;
	}
	.panel {
		border: 1px solid var(--border-soft);
		border-radius: 12px;
		background: var(--surface);
	}
	.bad {
		margin: 0 0 10px;
		font-size: 12.5px;
		color: var(--red);
	}
	.note {
		margin: 14px 0 0;
		font-size: 12.5px;
		color: var(--t2);
	}
	.btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: 8px;
		height: 32px;
		font: inherit;
		font-size: 13px;
		font-weight: 500;
		padding: 0 12px;
		border-radius: 7px;
		border: 1px solid var(--border);
		background: var(--surface-2);
		color: var(--text);
		white-space: nowrap;
		text-decoration: none;
		cursor: pointer;
	}
	.btn:hover:not(:disabled) {
		border-color: var(--t2);
	}
	.btn.sm {
		height: 26px;
		font-size: 12.5px;
		padding: 0 10px;
	}
	.btn.primary {
		background: var(--accent-strong);
		border-color: var(--accent-strong);
		color: #fff;
	}
	.btn.primary:hover:not(:disabled) {
		background: var(--accent);
		border-color: var(--accent);
	}
	.btn.quiet {
		background: transparent;
		border-color: transparent;
		color: var(--t2);
	}
	.btn.quiet:hover {
		color: var(--text);
	}
	.btn:disabled {
		opacity: 0.55;
		cursor: default;
	}
	.btn:focus-visible,
	.quiet-link:focus-visible,
	.ico:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.empty {
		display: flex;
		align-items: center;
		gap: 12px;
		color: var(--red);
	}

	/* Step 1 */
	.more {
		margin-top: 14px;
	}
	.accts {
		list-style: none;
		margin: 0 0 14px;
		padding: 0;
	}
	.accts li {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 12px 14px;
	}
	.accts li + li {
		border-top: 1px solid var(--border-soft);
	}
	.accts b {
		font-weight: 500;
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.chip {
		display: inline-flex;
		align-items: center;
		padding: 1px 8px;
		border-radius: 999px;
		font-size: 12px;
		white-space: nowrap;
	}
	.chip.green {
		background: var(--green-soft);
		color: var(--green);
	}

	/* Step 2 */
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
		padding: 0;
		color: var(--text);
		font: inherit;
		caret-color: var(--accent);
	}
	.search input::placeholder {
		color: var(--t3);
	}
	.pick .bad,
	.pick .note {
		margin: 8px 0 0;
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
		padding: 6px 8px;
		border-radius: 6px;
	}
	.sugg li:hover {
		background: var(--surface-3);
	}
	.sugg b {
		font-weight: 500;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.art {
		width: 28px;
		height: 37px;
		border-radius: 4px;
		flex: none;
		display: grid;
		place-items: center;
		background: var(--art-bg);
		color: var(--t2);
		font: 600 12px var(--font);
	}
	.list-h {
		margin: 28px 0 8px;
		font-size: 12.5px;
		font-weight: 600;
		color: var(--t2);
	}
	.empty-list {
		margin: 0;
		font-size: 13px;
	}
	.order {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.order li {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 10px 12px;
	}
	.order li + li {
		border-top: 1px solid var(--border-soft);
	}
	.rank {
		width: 16px;
		text-align: right;
		color: var(--accent);
		font-size: 13px;
		font-weight: 600;
	}
	.thumb {
		width: 28px;
		flex: none;
	}
	.gm {
		display: grid;
	}
	.gm b {
		font-weight: 500;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.meta {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font-size: 12px;
	}
	.moves {
		display: flex;
		gap: 2px;
	}
	.ico {
		width: 28px;
		height: 28px;
		display: grid;
		place-items: center;
		background: none;
		border: 0;
		border-radius: 6px;
		color: var(--t2);
		cursor: pointer;
	}
	.ico:hover:not(:disabled) {
		background: var(--surface-2);
		color: var(--text);
	}
	.ico:disabled {
		opacity: 0.35;
		cursor: default;
	}
	.ico svg {
		width: 15px;
		height: 15px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.8;
		stroke-linecap: round;
		stroke-linejoin: round;
	}

	.foot {
		display: flex;
		align-items: center;
		gap: 8px;
		margin-top: 28px;
		padding-top: 16px;
		border-top: 1px solid var(--border-soft);
	}

	@media (max-width: 560px) {
		.top {
			padding: 0 16px;
		}
		.wiz {
			padding: 24px 16px 48px;
		}
		.meta {
			display: none;
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.segs li {
			transition: none;
		}
	}
</style>
