<script lang="ts">
	import { onDestroy } from 'svelte';
	import { api } from '$lib/app';
	import type { Game, SearchHit } from '$lib/api/types';
	import BoxArt from '$lib/components/BoxArt.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import LinkRecheck from '$lib/components/LinkRecheck.svelte';
	import PageHead from '$lib/components/PageHead.svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { relTime } from '$lib/format';
	import { byEndingSoonest, campaignLine, commitGames, dupePriority, linkErrorKey, linkHref, matchParts, moveGame, moveTarget, suggestions, withoutId } from '$lib/games';
	import { t, tn } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';
	import { applyPrefs, prefs } from '$lib/prefs.svelte';
	import type { Settings } from '$lib/api/types';
	import { scanStamp } from '$lib/scan';
	import { reorder } from '$lib/drag';
	import AccountLists from './AccountLists.svelte';
	import ForceWatch from './ForceWatch.svelte';
	import CampaignDrawer, { type CampaignTarget } from '$lib/components/CampaignDrawer.svelte';

	const live = getLive();

	let drawer = $state<CampaignTarget | null>(null);

	let now = $state(Date.now());
	$effect(() => {
		const id = setInterval(() => (now = Date.now()), 5000);
		return () => clearInterval(id);
	});

	let q = $state('');
	let hits = $state<SearchHit[]>([]);
	let searchFailed = $state(false);
	let failed = $state(false);
	let modeBusy = $state(false);
	let modeFailed = $state(false);

	/** Saves the drop priority mode, the same setting as Settings, Engine. */
	async function setMode(mode: Settings['priority_mode']): Promise<void> {
		if (modeBusy || prefs.priorityMode === mode) return;
		modeBusy = true;
		modeFailed = false;
		try {
			applyPrefs(await api.saveSettings({ priority_mode: mode }));
		} catch {
			modeFailed = true;
		} finally {
			modeBusy = false;
		}
	}
	let dupe = $state<number | null>(null);
	let linkErr = $state<string | null>(null);
	let busy = $state<string | null>(null);
	let confirming = $state<string | null>(null);
	let moved = $state<string | null>(null);
	let saving = $state(false);
	let drag = $state<{ id: string; from: number; to: number } | null>(null);
	let timer: ReturnType<typeof setTimeout> | undefined;
	let flash: ReturnType<typeof setTimeout> | undefined;
	let seq = 0;

	type View = 'cards' | 'list';
	const VIEW_KEY = 'grub.games.view';
	let view = $state<View>('cards');
	try {
		if (localStorage.getItem(VIEW_KEY) === 'list') view = 'list';
	} catch {
		/* storage blocked: keep the default */
	}
	function setView(v: View): void {
		view = v;
		try {
			localStorage.setItem(VIEW_KEY, v);
		} catch {
			/* storage blocked: the choice lasts for this visit */
		}
	}

	const games = $derived(live.games?.games ?? []);
	const lastScan = $derived(live.games?.last_scan_at ?? null);
	const nextText = $derived(
		scanStamp(lastScan, live.games?.next_scan_at ?? null, new Date(now))
			.filter((x) => x.key !== 'home.last_scan')
			.map((x) => t(x.key, x.vars))
			.join(' · ')
	);
	const ending = $derived(prefs.priorityMode === 'ending_soonest');
	const rows = $derived(ending ? byEndingSoonest(games) : drag ? moveGame(games, drag.from, drag.to) : games);
	const sugg = $derived(suggestions(hits, q));
	const head = $derived(
		live.games ? tn('games.head', games.length, { rel: relTime(lastScan, now) }) : undefined
	);

	// Busy until a scan lands or SCAN_WAIT_MS runs out.
	const SCAN_WAIT_MS = 90_000;
	let scanFrom = $state<string | null | undefined>(undefined);
	let scanFailed = $state(false);
	let scanTimer: ReturnType<typeof setTimeout> | undefined;
	const scanning = $derived(scanFrom !== undefined && lastScan === scanFrom);
	async function scanNow(): Promise<void> {
		scanFailed = false;
		scanFrom = lastScan;
		clearTimeout(scanTimer);
		try {
			await api.scanGames();
		} catch {
			scanFailed = true;
			scanFrom = undefined;
			return;
		}
		scanTimer = setTimeout(() => (scanFrom = undefined), SCAN_WAIT_MS);
	}

	function onInput(): void {
		clearTimeout(timer);
		searchFailed = false;
		dupe = null;
		linkErr = null;
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

	async function reload(): Promise<void> {
		try {
			live.games = await api.games();
		} catch {
			/* the games event refetches */
		}
	}

	async function add(name: string): Promise<void> {
		busy = name;
		failed = false;
		dupe = null;
		linkErr = null;
		try {
			await api.addGame(name);
			q = '';
			hits = [];
			seq++;
		} catch (e) {
			dupe = dupePriority(e);
			linkErr = linkErrorKey(e);
			failed = dupe === null && linkErr === null;
		} finally {
			busy = null;
		}
		await reload();
	}

	async function commit(next: Game[], id?: string): Promise<void> {
		if (saving) return;
		saving = true;
		failed = false;
		confirming = null;
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
		if (!ok) failed = true;
		if (ok && id) {
			moved = id;
			clearTimeout(flash);
			flash = setTimeout(() => (moved = null), 600);
		}
	}

	function remove(id: string): Promise<void> {
		const keep = withoutId(
			games.map((g) => g.id),
			id
		);
		return commit(keep.map((k) => games.find((g) => g.id === k)!));
	}

	const dragOpts = {
		enabled: () => !saving && !ending,
		onStart: (from: number) => (drag = { id: games[from].id, from, to: from }),
		onMove: (to: number) => {
			if (drag) drag.to = to;
		},
		onEnd: (from: number, to: number) => {
			const d = drag;
			drag = null;
			if (d && from !== to) void commit(moveGame(games, from, to), d.id);
		},
		onCancel: () => (drag = null)
	};

	function stateText(g: Game): string {
		return g.state === 'mining'
			? t('home.state_mining')
			: g.state === 'waiting'
				? t('home.state_waiting')
				: g.state === 'done'
					? t('home.state_done')
					: t('home.state_none');
	}

	onDestroy(() => {
		clearTimeout(scanTimer);
		clearTimeout(timer);
		clearTimeout(flash);
	});
</script>

<PageHead title={t('nav.games')} status={head}>
	{#snippet action()}
		<span class="scan-now">
			<div class="seg" role="group" aria-label={t('games.view_label')}>
				<button type="button" aria-pressed={view === 'cards'} onclick={() => setView('cards')}>{t('games.view_cards')}</button>
				<button type="button" aria-pressed={view === 'list'} onclick={() => setView('list')}>{t('games.view_list')}</button>
			</div>
			{#if scanFailed}<small class="bad" role="alert">{t('games.scan_failed')}</small>{/if}
			<button type="button" class="btn" disabled={scanning} onclick={scanNow}>
				<Icon name="refresh" />{scanning ? t('games.scanning') : t('games.scan_now')}
			</button>
		</span>
	{/snippet}
	<div class="cols with-rail">
		<div class="side">
			<aside class="rail" aria-labelledby="pm-g">
				<h2 id="pm-g" class="rh">{t('settings.priority_mode')}</h2>
				<div class="seg pm" role="group" aria-labelledby="pm-g">
					<button type="button" aria-pressed={prefs.priorityMode === 'ordered'} disabled={modeBusy} onclick={() => setMode('ordered')}>
						{t('settings.pm_ordered')}
					</button>
					<button type="button" aria-pressed={prefs.priorityMode === 'ending_soonest'} disabled={modeBusy} onclick={() => setMode('ending_soonest')}>
						{t('settings.pm_ending_soonest')}
					</button>
				</div>
				{#if modeFailed}<p class="err" role="alert">{t('settings.save_failed')}</p>{/if}
			</aside>
			{#if live.accounts.length > 0}
				<AccountLists />
				<ForceWatch />
			{/if}
		</div>
		<div class="main">
			<section class="add" aria-label={t('games.add_label')}>
				<label class="search">
					<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg>
					<input
						type="search"
						bind:value={q}
						oninput={onInput}
						placeholder={t('games.add_ph')}
						aria-label={t('games.add_label')}
						autocomplete="off"
					/>
				</label>
				{#if sugg.length > 0}
					<ul class="sugg">
						{#each sugg as s (s.kind === 'hit' ? `h:${s.hit.id}` : 'add')}
							<li>
								{#if s.kind === 'hit'}
									{@const m = matchParts(s.hit.name, q)}
									<span class="thumb" aria-hidden="true"><BoxArt src={s.hit.box_art_url || null} name={s.hit.name} size="sm" /></span>
									<span class="nm">
										<b>{m.pre}{#if m.hit}<mark>{m.hit}</mark>{/if}{m.post}</b>
										{#if s.hit.chosen && s.hit.priority !== null}
											<small>{t('games.already', { n: s.hit.priority })}</small>
										{/if}
									</span>
									<button
										type="button"
										class="add-btn"
										disabled={s.hit.chosen || busy !== null}
										onclick={() => add(s.hit.name)}>{s.hit.chosen ? t('games.added') : t('games.add')}</button
									>
								{:else}
									<span class="thumb" aria-hidden="true">+</span>
									<span class="nm"><b>{t('games.add_named', { name: s.name })}</b></span>
									<button type="button" class="add-btn" disabled={busy !== null} onclick={() => add(s.name)}>{t('games.add')}</button>
								{/if}
							</li>
						{/each}
					</ul>
				{:else}
					<p class="eg">{t('games.add_example', { url: 'https://www.twitch.tv/directory/category/rust' })}</p>
				{/if}
				{#if dupe !== null}<p class="note" role="status">{t('games.already', { n: dupe })}</p>{/if}
				{#if linkErr}<p class="err" role="alert">{t(linkErr)}</p>{/if}
				{#if searchFailed}<p class="err" role="alert">{t('home.search_failed')}</p>{/if}
			</section>

			{#if failed}<p class="err" role="alert">{t('games.save_failed')}</p>{/if}

			{#if live.games && games.length === 0}
				<p class="empty">{t('games.empty')}</p>
			{/if}
			<div class="list" class:compact={view === 'list'} use:reorder={dragOpts}>
				{#each rows as g, i (g.id)}
					{#if view === 'list'}
						<div class="game row" class:moved={moved === g.id} class:dragging={drag?.id === g.id}>
							<span class="rk">{String(i + 1).padStart(2, '0')}</span>
							{@render grip(g, i)}
							<span class="rname">{g.name}</span>
						</div>
					{:else}
					<article class="game" class:moved={moved === g.id} class:dragging={drag?.id === g.id}>
						{@render grip(g, i)}
						<div class="artw">
							<BoxArt src={g.box_art_url} name={g.name} size="lg" />
							<span class="rank">{String(i + 1).padStart(2, '0')}</span>
						</div>
						<div class="body">
							<div class="status">
								<h3 class="gname">{g.name}</h3>
								<span class="state {g.state}">{stateText(g)}</span>
								{#if confirming === g.id}
									<span class="conf">
										<span>{t('games.remove_confirm', { game: g.name })}</span>
										<button type="button" class="danger" onclick={() => remove(g.id)}>{t('games.remove')}</button>
										<button type="button" class="plain" onclick={() => (confirming = null)}>{t('games.cancel')}</button>
									</span>
								{:else}
									<button type="button" class="rm" onclick={() => (confirming = g.id)}>
										<Icon name="x" size={14} />{t('games.remove')}
									</button>
								{/if}
							</div>
							{#if g.campaigns.length > 0}
								<ul class="camps">
									{#each g.campaigns as c (`${c.platform}/${c.id}`)}
										<li>
											<button
												type="button"
												class="camp"
												data-campaign
												aria-haspopup="dialog"
												title={t('campaign.open')}
												onclick={() => (drawer = { platform: c.platform, id: c.id })}
											>
												<PlatformMark platform={c.platform} /><span>{c.name}</span><small>{campaignLine(c, prefs.zone)}</small>
												<svg class="chev" viewBox="0 0 12 12" aria-hidden="true"><path d="m4.5 2.5 3.5 3.5-3.5 3.5" /></svg>
											</button>
										</li>
									{/each}
								</ul>
							{:else}
								<p class="muted">{t('games.nothing_live', { rel: relTime(lastScan, now) })}{#if nextText} · {nextText}{/if}</p>
							{/if}
							{#if g.needs_link?.length}
								<ul class="links">
									{#each g.needs_link as n (n.account_id)}
										{@const href = linkHref(n.url)}
										<li>
											<Icon name="alert" size={14} />
											<span>{t('games.needs_link', { account: n.account_name })}</span>
											{#if href}
												<a {href} target="_blank" rel="noopener noreferrer">{t('games.link_account')}</a>
											{/if}
											<LinkRecheck accountIds={[n.account_id]} />
										</li>
									{/each}
								</ul>
							{/if}
						</div>
					</article>
					{/if}
				{/each}
			</div>
		</div>
	</div>
</PageHead>

<CampaignDrawer target={drawer} onclose={() => (drawer = null)} />

	{#snippet grip(g: Game, i: number)}
		{#if ending}
			<div class="grip locked" title={t('settings.pm_ending_soonest')}>
				<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" /></svg>
			</div>
		{:else}
			<div class="grip">
				<button type="button" aria-label={t('games.move_up')} title={t('games.move_up')} disabled={i === 0 || saving} onclick={(e) => commit(moveGame(games, i, moveTarget(games.length, i, -1, e.shiftKey)), g.id)}>
					<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 15 6-6 6 6" /></svg>
				</button>
				<span
					class="h"
					title={t('games.drag')}
					role="presentation"
				>
					<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M9 6h.01M15 6h.01M9 12h.01M15 12h.01M9 18h.01M15 18h.01" stroke-width="3" /></svg>
				</span>
				<button type="button" aria-label={t('games.move_down')} title={t('games.move_down')} disabled={i === rows.length - 1 || saving} onclick={(e) => commit(moveGame(games, i, moveTarget(games.length, i, 1, e.shiftKey)), g.id)}>
					<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg>
				</button>
			</div>
		{/if}
	{/snippet}

<style>
	.cols.with-rail {
		display: grid;
		grid-template-columns: 250px minmax(0, 1fr);
		gap: 22px;
		align-items: start;
	}
	.side {
		display: grid;
		gap: 12px;
		position: sticky;
		top: 12px;
		max-height: calc(100dvh - 24px);
		overflow-y: auto;
	}
	@media (max-width: 860px) {
		.cols.with-rail {
			grid-template-columns: minmax(0, 1fr);
		}
		.side {
			position: static;
			max-height: none;
			overflow: visible;
		}
	}
	.scan-now {
		display: inline-flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 10px;
	}
	.scan-now small {
		font-size: 12px;
		color: var(--red);
	}
	.btn {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font-size: 13px;
		font-weight: 500;
		background: var(--surface-2);
		border: 1px solid var(--border);
		border-radius: 8px;
		height: 32px;
		padding: 0 14px;
		color: var(--text);
		white-space: nowrap;
		cursor: pointer;
	}
	.btn:hover:not([disabled]) {
		border-color: var(--t3);
		background: var(--surface-3);
	}
	.btn[disabled] {
		opacity: 0.45;
		cursor: default;
	}
	.btn:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.i {
		width: 14px;
		height: 14px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.8;
		stroke-linecap: round;
		stroke-linejoin: round;
		flex: none;
	}
	.add {
		position: relative;
		margin-bottom: 14px;
	}
	.search {
		display: flex;
		align-items: center;
		gap: 10px;
		height: 42px;
		padding: 0 14px;
		border: 1px solid var(--border);
		border-radius: 10px;
		background: var(--surface);
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
	.eg {
		margin: 6px 2px 0;
		font-size: 11.5px;
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
	.sugg mark {
		background: none;
		color: var(--accent);
	}
	.sugg li:hover {
		background: var(--surface-3);
	}
	.thumb {
		width: 28px;
		height: 37px;
		overflow: hidden;
		border-radius: 4px;
		flex: none;
		display: grid;
		place-items: center;
		background: var(--art-bg);
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
	.nm small {
		color: var(--t3);
		font-size: 12px;
	}
	.add-btn {
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
	.note {
		margin: 8px 2px 0;
		font-size: 12.5px;
		color: var(--t2);
	}
	.err {
		margin: 0 0 10px;
		font-size: 12px;
		color: var(--red);
	}
	.add .err {
		margin: 8px 2px 0;
	}
	.rail {
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		padding: 8px;
	}
	.rh {
		margin: 8px 8px 4px;
		font-size: 11px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--t3);
	}
	.seg.pm {
		display: grid;
		grid-template-columns: 1fr 1fr;
		margin: 0 8px 6px;
	}
	.seg.pm button {
		min-width: 0;
		padding: 4px 6px;
		font-size: 12px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.rail .err {
		margin: 0 8px 6px;
	}
	.seg button:disabled {
		cursor: default;
	}
	.seg {
		display: inline-flex;
		border: 1px solid var(--border);
		border-radius: 8px;
		overflow: hidden;
	}
	.seg button {
		font: inherit;
		font-size: 12.5px;
		padding: 0 12px;
		height: 30px;
		border: 0;
		background: var(--surface);
		color: var(--t2);
		cursor: pointer;
	}
	.seg button + button {
		border-left: 1px solid var(--border);
	}
	.seg button[aria-pressed='true'] {
		background: var(--accent-soft);
		color: var(--accent);
	}
	.list.compact .game + .game {
		margin-top: 4px;
	}
	.game.row {
		grid-template-columns: 40px auto minmax(0, 1fr);
		align-items: center;
		height: 38px;
		border-radius: 8px;
	}
	.grip.locked {
		color: var(--accent);
	}
	.row .rk {
		text-align: center;
		font: 500 12px/1 var(--mono);
		color: var(--t3);
	}
	.row .grip {
		flex-direction: row;
		border-right: 0;
		gap: 0;
	}
	.row .rname {
		padding: 0 12px;
		font-size: 14px;
		font-weight: 500;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.game {
		display: grid;
		grid-template-columns: 36px 116px minmax(0, 1fr);
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		overflow: hidden;
		transition: border-color 0.15s;
	}
	.game + .game {
		margin-top: 10px;
	}
	.game.moved {
		border-color: var(--accent);
	}
	.game.dragging {
		border-color: var(--accent);
		background: var(--surface-2);
		box-shadow: 0 8px 24px rgb(0 0 0 / 0.35);
		position: relative;
		z-index: 1;
	}
	.grip {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 2px;
		border-right: 1px solid var(--border-soft);
		color: var(--t3);
	}
	.h {
		cursor: grab;
		padding: 2px 0;
		touch-action: none;
		display: grid;
	}
	.grip button {
		width: 26px;
		height: 22px;
		display: grid;
		place-items: center;
		border: 0;
		background: none;
		border-radius: 4px;
		color: var(--t2);
		cursor: pointer;
	}
	.grip button:hover:not(:disabled) {
		background: var(--surface-3);
		color: var(--text);
	}
	.grip button:disabled {
		opacity: 0.3;
		cursor: default;
	}
	.artw {
		position: relative;
		overflow: hidden;
		isolation: isolate;
		display: grid;
		min-height: 112px;
	}
	.artw::after {
		content: '';
		position: absolute;
		left: 0;
		right: 0;
		bottom: 0;
		height: 45%;
		z-index: 1;
		background: linear-gradient(180deg, transparent, rgba(0, 0, 0, 0.6));
		pointer-events: none;
	}
	.rank {
		position: absolute;
		left: 8px;
		bottom: 6px;
		z-index: 2;
		font: 500 12px/1 var(--mono);
		color: rgba(255, 255, 255, 0.9);
	}
	.artw :global(.art) {
		min-height: 112px;
	}
	.body {
		padding: 14px 18px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		justify-content: center;
		gap: 10px;
	}
	.status {
		display: flex;
		align-items: center;
		gap: 6px 8px;
		flex-wrap: wrap;
	}
	.gname {
		margin: 0 4px 0 0;
		font-size: 15px;
		font-weight: 600;
		letter-spacing: -0.01em;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		min-width: 0;
	}
	.state {
		font-size: 12px;
		font-weight: 500;
		padding: 2px 8px;
		border-radius: 10px;
		display: inline-flex;
		align-items: center;
		gap: 6px;
		white-space: nowrap;
	}
	.state::before {
		content: '';
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: currentColor;
	}
	.state.mining {
		color: var(--green);
		background: var(--green-soft);
	}
	.state.waiting {
		color: #b48cf8;
		background: var(--accent-soft);
	}
	.state.no_campaign {
		color: var(--t2);
		background: var(--surface-3);
	}
	.state.done {
		color: var(--green);
		background: var(--surface-3);
	}
	.rm {
		margin-left: auto;
		color: var(--t3);
		font-size: 12px;
		display: inline-flex;
		align-items: center;
		gap: 4px;
		background: none;
		border: 0;
		padding: 2px 4px;
		border-radius: 4px;
		cursor: pointer;
	}
	.rm:hover {
		color: var(--red);
	}
	.conf {
		margin-left: auto;
		display: inline-flex;
		align-items: center;
		gap: 8px;
		font-size: 12px;
		color: var(--text);
		flex-wrap: wrap;
	}
	.conf button {
		font-size: 12px;
		border-radius: 6px;
		padding: 2px 8px;
		border: 1px solid var(--border);
		background: none;
		color: var(--t2);
		cursor: pointer;
	}
	.conf .danger {
		color: var(--red);
		border-color: color-mix(in srgb, var(--red) 45%, transparent);
	}
	.conf button:hover {
		color: var(--text);
		border-color: var(--t2);
	}
	.camps {
		margin: 0;
		padding: 0;
		list-style: none;
		display: grid;
		gap: 6px;
	}
	.camp {
		width: 100%;
		display: grid;
		grid-template-columns: 14px 1fr auto 12px;
		gap: 10px;
		align-items: center;
		margin: -4px -6px;
		padding: 4px 6px;
		box-sizing: content-box;
		border: 0;
		border-radius: 6px;
		background: none;
		color: inherit;
		font: inherit;
		font-size: 13px;
		text-align: left;
		cursor: pointer;
	}
	.camp:hover {
		background: var(--surface-2);
	}
	.camp:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 0;
	}
	.chev {
		width: 12px;
		height: 12px;
		fill: none;
		stroke: var(--t3);
		stroke-width: 1.6;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
	.camp:hover .chev {
		stroke: var(--text);
	}
	.camps li span:nth-child(2) {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.camps li small {
		color: var(--t3);
		font: 12px var(--mono);
		white-space: nowrap;
	}
	.links {
		margin: 10px 0 0;
		padding: 0;
		list-style: none;
		display: grid;
		gap: 6px;
	}
	.links li {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 4px 8px;
		color: var(--amber);
		font-size: 13px;
	}
	.links a {
		color: var(--accent);
		font-weight: 500;
	}
	.links a:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.muted {
		color: var(--t2);
		font-size: 13px;
		margin: 0;
	}
	.empty {
		padding: 28px 16px;
		text-align: center;
		color: var(--t3);
		font-size: 13px;
		border: 1px dashed var(--border);
		border-radius: 10px;
		margin: 0;
	}
	@media (max-width: 720px) {
		.game {
			grid-template-columns: 36px 64px minmax(0, 1fr);
		}
		.artw,
		.artw :global(.art) {
			min-height: 0;
		}
		.body {
			padding: 12px 14px;
		}
		.camp {
			grid-template-columns: 14px 1fr 12px;
		}
		.camps li small {
			grid-column: 2;
			grid-row: 2;
			margin-top: -4px;
		}
	}
</style>
