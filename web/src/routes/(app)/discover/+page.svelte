<script lang="ts">
	import { untrack } from 'svelte';
	import { api } from '$lib/app';
	import type { DiscoverResp, Platform } from '$lib/api/types';
	import BoxArt from '$lib/components/BoxArt.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import PageHead from '$lib/components/PageHead.svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { filterGroups, groupByGame, minutesLeft } from '$lib/discover';
	import { formatInt, relTime, shortDate } from '$lib/format';
	import { linkHref } from '$lib/games';
	import { t, tn } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';
	import { prefs } from '$lib/prefs.svelte';

	const live = getLive();

	let now = $state(Date.now());
	$effect(() => {
		const id = setInterval(() => (now = Date.now()), 5000);
		return () => clearInterval(id);
	});

	let data = $state<DiscoverResp | null>(null);
	let busy = $state(false);
	let failed = $state(false);
	let q = $state('');
	let platform = $state<'all' | Platform>('all');
	let hideAdded = $state(true);
	let adding = $state<string | null>(null);
	let addFailed = $state<string | null>(null);

	const chosen = $derived(live.games?.games ?? []);
	const groups = $derived(groupByGame(data?.campaigns ?? [], chosen));
	const shown = $derived(filterGroups(groups, { q, platform, hideAdded }));
	const head = $derived(data ? tn('discover.head', groups.length) : undefined);
	const stamp = $derived(data?.fetched_at ? t('discover.fetched', { rel: relTime(data.fetched_at, now) }) : undefined);
	const canRefresh = $derived(
		!busy && (!data?.next_refresh_at || Date.parse(data.next_refresh_at) <= now)
	);
	const twitchState = $derived(data?.platforms.twitch);
	const kickState = $derived(data?.platforms.kick);

	async function load(refresh = false): Promise<void> {
		busy = true;
		failed = false;
		try {
			data = await api.discover(refresh);
		} catch {
			failed = true;
		} finally {
			busy = false;
		}
	}

	$effect(() => {
		untrack(() => void load());
	});

	async function add(name: string): Promise<void> {
		adding = name;
		addFailed = null;
		try {
			await api.addGame(name);
		} catch {
			addFailed = name;
		} finally {
			adding = null;
		}
		try {
			live.games = await api.games();
		} catch {
			/* the games event refetches */
		}
	}

	function left(iso: string | null): string {
		const m = minutesLeft(iso, now);
		if (m === null) return t('discover.no_end');
		if (m >= 2 * 1440) return t('discover.left_d', { n: Math.floor(m / 1440) });
		if (m >= 60) return t('discover.left_h', { n: Math.floor(m / 60) });
		return t('discover.left_m', { n: m });
	}

	const name = (p: Platform) => (p === 'twitch' ? 'Twitch' : 'Kick');
</script>

<PageHead title={t('nav.discover')} status={head} {stamp}>
	{#snippet action()}
		<button type="button" class="btn" disabled={!canRefresh} onclick={() => load(true)}>
			<Icon name="refresh" />{busy && data ? t('discover.refreshing') : t('discover.refresh')}
		</button>
	{/snippet}

	{#if busy && data}{@render loader()}{/if}
	{#if data}
		{#if twitchState?.error === 'needs_android'}
			<p class="note" role="status">
				<PlatformMark platform="twitch" size="sm" />{t('discover.needs_android')}
				<a href="/settings#accounts">{t('discover.needs_android_link')}</a>
			</p>
		{/if}
		{#each [['twitch', twitchState], ['kick', kickState]] as const as [p, st] (p)}
			{#if st && !st.ok && st.error !== 'needs_android'}
				<p class="note bad" role="alert">
					<Icon name="alert" size={14} />{st.error === 'timeout'
						? t('discover.platform_timeout', { platform: name(p) })
						: t('discover.platform_failed', { platform: name(p) })}
				</p>
			{/if}
		{/each}

		<div class="filters">
			<input
				class="search"
				type="search"
				placeholder={t('discover.search_ph')}
				aria-label={t('discover.search_ph')}
				bind:value={q}
			/>
			<div class="fgroup" role="group" aria-label={t('discover.filter_platform')}>
				<span>{t('discover.filter_platform')}</span>
				<button type="button" class="chip" class:on={platform === 'all'} aria-pressed={platform === 'all'} onclick={() => (platform = 'all')}
					>{t('discover.all')}</button
				>
				{#each ['twitch', 'kick'] as const as p (p)}
					<button type="button" class="chip" class:on={platform === p} aria-pressed={platform === p} onclick={() => (platform = p)}
						><PlatformMark platform={p} size="sm" />{name(p)}</button
					>
				{/each}
			</div>
			<label class="toggle">
				<input type="checkbox" bind:checked={hideAdded} />
				<span>{t('discover.hide_added')}</span>
			</label>
		</div>

		{#if shown.length === 0}
			<p class="empty">{groups.length === 0 ? t('discover.empty') : t('discover.no_match')}</p>
		{/if}
		<div class="list">
			{#each shown as g (g.key)}
				<article class="game">
					<div class="artw"><BoxArt src={g.box_art_url || null} name={g.name} size="lg" /></div>
					<div class="body">
						<div class="status">
							<h3 class="gname">{g.name}</h3>
							<span class="pfs">{#each g.platforms as p (`${g.key}:${p}`)}<PlatformMark platform={p} size="sm" />{/each}</span>
							<button
								type="button"
								class="add-btn"
								disabled={g.added || adding !== null}
								onclick={() => add(g.name)}>{g.added ? t('discover.added') : t('discover.add')}</button
							>
						</div>
						{#if addFailed === g.name}<p class="err" role="alert">{t('discover.add_failed', { game: g.name })}</p>{/if}
						<ul class="camps">
							{#each g.campaigns as c (`${c.platform}/${c.id}`)}
								{@const href = linkHref(c.link_url)}
								<li>
									<div class="ch">
										<PlatformMark platform={c.platform} />
										<span class="cn">{c.name}</span>
										<small title={c.ends_at ? t('discover.ends', { date: shortDate(c.ends_at, prefs.zone) }) : undefined}
											>{left(c.ends_at)}{#if c.ends_at} · {t('discover.ends', { date: shortDate(c.ends_at, prefs.zone) })}{/if}</small
										>
									</div>
									{#if c.needs_link}
										<p class="link">
											<Icon name="alert" size={14} /><span>{t('discover.needs_link')}</span>
											{#if href}<a {href} target="_blank" rel="noopener noreferrer">{t('discover.link')}</a>{/if}
										</p>
									{/if}
									{#if c.rewards.length > 0}
										<ul class="rewards">
											{#each c.rewards as r, i (`${i}:${r.name}`)}
												<li>
													<span class="img"><BoxArt src={r.image_url || null} name={r.name} size="sm" /></span>
													<span class="rn">{r.name}</span>
													{#if r.minutes > 0}<small>{t('discover.minutes', { n: formatInt(r.minutes) })}</small>{/if}
												</li>
											{/each}
										</ul>
									{:else}
										<p class="muted">{t('discover.no_rewards')}</p>
									{/if}
								</li>
							{/each}
						</ul>
					</div>
				</article>
			{/each}
		</div>
	{:else if failed}
		<div class="empty err" role="status">
			{t('discover.failed')}
			<button type="button" class="btn" disabled={busy} onclick={() => load()}>{t('discover.retry')}</button>
		</div>
	{:else}
		<div class="empty">{@render loader()}</div>
	{/if}
	{#if data && failed}<p class="note bad" role="alert"><Icon name="alert" size={14} />{t('discover.failed')}</p>{/if}
</PageHead>

{#snippet loader()}
	<div class="loading" role="status">
		<div class="lbar" aria-hidden="true"><i></i></div>
		<span>{t('discover.loading')}</span>
	</div>
{/snippet}

<style>
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
	.btn:focus-visible,
	.chip:focus-visible,
	.add-btn:focus-visible,
	.search:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.note {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 6px 8px;
		margin: 0 0 10px;
		font-size: 13px;
		color: var(--t2);
	}
	.note.bad {
		color: var(--red);
	}
	.note a,
	.link a {
		color: var(--accent);
		font-weight: 500;
	}
	.filters {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 16px;
		margin-bottom: 14px;
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
		cursor: pointer;
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
	.toggle {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font-size: 12px;
		color: var(--t2);
		cursor: pointer;
	}
	.toggle input {
		accent-color: var(--accent);
		margin: 0;
	}
	.game {
		display: grid;
		grid-template-columns: 116px minmax(0, 1fr);
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		overflow: hidden;
	}
	.game + .game {
		margin-top: 10px;
	}
	.artw {
		position: relative;
		overflow: hidden;
		display: grid;
		min-height: 112px;
	}
	.artw :global(.art) {
		min-height: 112px;
	}
	.body {
		padding: 14px 18px;
		min-width: 0;
		display: flex;
		flex-direction: column;
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
	.pfs {
		display: inline-flex;
		gap: 4px;
	}
	.add-btn {
		margin-left: auto;
		font-size: 12.5px;
		font-weight: 500;
		padding: 4px 10px;
		border-radius: 6px;
		border: 1px solid var(--border);
		background: var(--surface);
		color: var(--text);
		cursor: pointer;
		white-space: nowrap;
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
	.camps,
	.rewards {
		margin: 0;
		padding: 0;
		list-style: none;
		display: grid;
	}
	.camps {
		gap: 12px;
	}
	.camps > li {
		display: grid;
		gap: 6px;
		min-width: 0;
	}
	.ch {
		display: grid;
		grid-template-columns: 14px minmax(0, 1fr) auto;
		gap: 10px;
		align-items: center;
		font-size: 13px;
	}
	.cn {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		font-weight: 500;
	}
	.ch small {
		color: var(--t3);
		font: 12px var(--mono);
		white-space: nowrap;
	}
	.link {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 4px 8px;
		margin: 0 0 0 24px;
		color: var(--amber);
		font-size: 12px;
	}
	.rewards {
		margin-left: 24px;
		grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
		gap: 6px 12px;
	}
	.rewards li {
		display: grid;
		grid-template-columns: 28px minmax(0, 1fr) auto;
		gap: 8px;
		align-items: center;
		font-size: 12px;
		min-width: 0;
	}
	.img {
		width: 28px;
		height: 28px;
		border-radius: 4px;
		overflow: hidden;
		display: block;
	}
	.img :global(.art) {
		min-height: 0;
	}
	.rn {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		color: var(--t2);
	}
	.rewards small {
		font: 12px var(--mono);
		color: var(--t3);
		white-space: nowrap;
	}
	.muted {
		margin: 0 0 0 24px;
		color: var(--t3);
		font-size: 12px;
	}
	.err {
		margin: 0;
		color: var(--red);
		font-size: 12px;
	}
	.loading {
		display: grid;
		gap: 8px;
		justify-items: center;
		margin: 0 0 10px;
		font-size: 13px;
		color: var(--t2);
	}
	.empty .loading {
		margin: 0;
	}
	.lbar {
		width: min(320px, 100%);
		height: 4px;
		border-radius: 2px;
		background: var(--surface-3);
		overflow: hidden;
	}
	.lbar i {
		display: block;
		width: 35%;
		height: 100%;
		border-radius: 2px;
		background: var(--accent);
		animation: slide 1.2s ease-in-out infinite;
	}
	@keyframes slide {
		from {
			transform: translateX(-100%);
		}
		to {
			transform: translateX(290%);
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.lbar i {
			width: 100%;
			animation: none;
			opacity: 0.5;
		}
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
	.empty.err {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 10px;
		color: var(--red);
	}
	@media (max-width: 720px) {
		.search {
			font-size: 16px;
		}
		.game {
			grid-template-columns: 64px minmax(0, 1fr);
		}
		.artw,
		.artw :global(.art) {
			min-height: 0;
		}
		.body {
			padding: 12px 14px;
		}
		.ch {
			grid-template-columns: 14px minmax(0, 1fr);
		}
		.ch small {
			grid-column: 2;
			margin-top: -4px;
		}
		.rewards,
		.link,
		.muted {
			margin-left: 0;
		}
	}
</style>
