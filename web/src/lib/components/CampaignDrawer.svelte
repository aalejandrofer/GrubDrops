<script lang="ts" module>
	import type { Platform } from '$lib/api/types';

	/** What the drawer shows: a campaign, and optionally the reward to highlight. */
	export type CampaignTarget = { platform: Platform; id: string; highlight?: string | null };
</script>

<script lang="ts">
	import { tick, untrack } from 'svelte';
	import { api } from '$lib/app';
	import { ApiError } from '$lib/api/client';
	import type { CampaignDetail, DropCell } from '$lib/api/types';
	import {
		campaignEnded,
		cellActions,
		cellKey,
		cellPct,
		claimedCount,
		needsLink,
		PENDING_MS,
		settlePending,
		withPending,
		type PendingMark
	} from '$lib/campaign';
	import { clock, shortDate } from '$lib/format';
	import { linkHref } from '$lib/games';
	import { t, tn } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';
	import { prefs } from '$lib/prefs.svelte';
	import ArmButton from './ArmButton.svelte';
	import BoxArt from './BoxArt.svelte';
	import PlatformMark from './PlatformMark.svelte';

	let { target, onclose }: { target: CampaignTarget | null; onclose: () => void } = $props();

	const live = getLive();
	/** Live refetches wait at least this long after the last load. */
	const REFETCH_MS = 10_000;
	/** While a hand mark waits for the runner's rescan, refetch this often. */
	const PENDING_REFETCH_MS = 2_000;

	let fetched = $state<CampaignDetail | null>(null);
	// Hand marks shown over the fetched cells until a rescan agrees.
	let pending = $state<PendingMark[]>([]);
	let markErr = $state('');
	const data = $derived(fetched ? withPending(fetched, pending) : null);
	let status = $state<'loading' | 'ok' | 'gone' | 'failed'>('loading');
	let closeBtn = $state<HTMLButtonElement | null>(null);
	let sheet = $state<HTMLElement | null>(null);
	let opener: HTMLElement | null = null;
	let lastLoad = 0;
	let seq = 0;

	const key = $derived(target ? `${target.platform}/${target.id}` : '');
	const hl = $derived(target?.highlight ?? null);
	const ended = $derived(data ? campaignEnded(data, Date.now()) : false);
	const href = $derived(data && needsLink(data) ? linkHref(data.link_url) : '');

	/** quiet keeps the current content on screen while refetching. */
	async function load(quiet = false): Promise<void> {
		const tg = target;
		if (!tg) return;
		const mine = ++seq;
		lastLoad = Date.now();
		if (!quiet) status = 'loading';
		try {
			const d = await api.campaign(tg.platform, tg.id);
			if (mine !== seq) return;
			fetched = d;
			pending = settlePending(d, pending, Date.now());
			status = 'ok';
		} catch (e) {
			if (mine !== seq || quiet) return;
			fetched = null;
			status = e instanceof ApiError && e.status === 404 ? 'gone' : 'failed';
		}
	}

	// A new campaign loads from scratch; the same one keeps its content.
	$effect(() => {
		if (!key) return;
		untrack(() => {
			fetched = null;
			pending = [];
			markErr = '';
			void load();
		});
	});

	// Runner pushes mean minutes moved: refetch, throttled.
	$effect(() => {
		void live.runners;
		untrack(() => {
			const wait = pending.length > 0 ? PENDING_REFETCH_MS : REFETCH_MS;
			if (key && status === 'ok' && Date.now() - lastLoad >= wait) void load(true);
		});
	});

	// Focus the close button on open and give focus back on close.
	let wasOpen = false;
	$effect(() => {
		const open = !!key;
		untrack(() => {
			if (open && !wasOpen) {
				opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
				void tick().then(() => closeBtn?.focus());
			} else if (!open && wasOpen) {
				opener?.focus();
				opener = null;
			}
			wasOpen = open;
		});
	});

	// Bring the highlighted reward into view once its row renders.
	$effect(() => {
		if (status !== 'ok' || !hl || !sheet) return;
		const el = sheet.querySelector<HTMLElement>('li.hl');
		el?.scrollIntoView?.({ block: 'nearest' });
	});

	function stamp(iso: string): string {
		return `${shortDate(iso, prefs.zone)} ${clock(iso, prefs.zone, prefs.fmt)}`;
	}

	function dates(d: CampaignDetail): string {
		if (d.starts_at && d.ends_at) return t('campaign.dates', { start: stamp(d.starts_at), end: stamp(d.ends_at) });
		if (d.ends_at) return t('campaign.ends', { date: stamp(d.ends_at) });
		return t('campaign.no_end');
	}

	function cellText(c: DropCell, req: number): string {
		if (c.state === 'claimed' && c.claimed_at) return t('campaign.claimed_on', { date: shortDate(c.claimed_at, prefs.zone) });
		return t(cellKey(c), { m: c.minutes, req });
	}

	/** Marks one account's drop collected or uncollected by hand. The drawer
	 * shows the mark at once; the runner reloads and its rescan follows. */
	async function mark(kind: PendingMark['kind'], dropId: string, c: DropCell): Promise<void> {
		markErr = '';
		try {
			if (kind === 'collect') await api.markCollected(c.account_id, dropId);
			else {
				await api.removeClaim(c.account_id, dropId);
				live.recent = live.recent.filter((x) => !(x.account_id === c.account_id && x.benefit_id === dropId));
			}
		} catch {
			markErr = kind === 'collect' ? t('mark.collect_failed') : t('mark.uncollect_failed');
			return;
		}
		const m: PendingMark = { account_id: c.account_id, drop_id: dropId, kind, at: new Date().toISOString(), until: Date.now() + PENDING_MS };
		pending = [...pending.filter((x) => !(x.account_id === m.account_id && x.drop_id === m.drop_id)), m];
		void load(true);
	}

	function onkey(e: KeyboardEvent): void {
		if (key && e.key === 'Escape') onclose();
	}

	// Clicks outside close the drawer. Clicks on another campaign trigger
	// (data-campaign) swap its content instead.
	function onpointer(e: PointerEvent): void {
		if (!key || !(e.target instanceof Element)) return;
		if (sheet?.contains(e.target) || e.target.closest('[data-campaign]')) return;
		onclose();
	}
</script>

<svelte:window onkeydown={onkey} onpointerdown={onpointer} />

{#if target}
	<div class="scrim" aria-hidden="true"></div>
	<div class="sheet" role="dialog" aria-modal="true" aria-label={data?.name ?? t('campaign.loading')} bind:this={sheet}>
		<button type="button" class="x" aria-label={t('campaign.close')} title={t('campaign.close')} bind:this={closeBtn} onclick={onclose}>
			<svg viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" aria-hidden="true"
				><path d="m3 3 6 6m0-6-6 6" /></svg
			>
		</button>
		{#if status === 'loading'}
			<p class="msg" role="status">{t('campaign.loading')}</p>
		{:else if status === 'gone'}
			<p class="msg">{t('campaign.gone')}</p>
		{:else if status === 'failed'}
			<p class="msg err" role="alert">
				{t('campaign.failed')}
				<button type="button" class="btn" onclick={() => load()}>{t('campaign.retry')}</button>
			</p>
		{:else if data}
			<header>
				<PlatformMark platform={data.platform} />
				<div>
					<h2>{data.name}</h2>
					<p>
						{data.game} · {dates(data)}{#if ended}
							· <span class="ended">{t('campaign.ended')}</span>{/if}
					</p>
				</div>
			</header>
			<p class="sum">{tn('campaign.summary', data.drops.length, { claimed: claimedCount(data) })}</p>
			{#if !data.live}<p class="sum">{t('campaign.from_claims')}</p>{/if}
			{#if markErr}<p class="msg err mark-err" role="alert">{markErr}</p>{/if}
			{#if href}
				<p class="link">
					<span>{t('campaign.link_banner')}</span>
					<a {href} target="_blank" rel="noopener noreferrer">{t('campaign.link')}</a>
				</p>
			{/if}
			<ol>
				{#each data.drops as d (d.id)}
					<li class:hl={hl === d.id}>
						<span class="img"><BoxArt src={d.image_url || null} name={d.name} size="sm" /></span>
						<div class="what">
							<span class="nm"
								>{d.name}{#if hl === d.id}<span class="you">{t('campaign.this_reward')}</span>{/if}</span
							>
							<!-- Claim-log rebuilds carry no minutes. -->
							{#if data.live || d.required_minutes > 0}
								<span class="req" class:dim={d.required_minutes <= 0}
									>{d.required_minutes > 0 ? t('campaign.minutes', { n: d.required_minutes }) : t('campaign.no_watch')}</span
								>
							{/if}
						</div>
						<div class="cells">
							{#each d.accounts as c (c.account_id)}
								{@const act = cellActions(c)}
								<span class="cell {c.state}">
									<span class="who"><PlatformMark platform={c.platform} size="sm" /><span>{c.account_name}</span></span>
									{#if c.state === 'accruing' || c.state === 'claimable'}
										<span class="bar"><i style="width:{cellPct(c, d.required_minutes)}%"></i></span>
									{/if}
									<span class="st">{cellText(c, d.required_minutes)}</span>
									<span class="acts">
										{#if act.collect}
											<ArmButton kind="collect" drop={d.name} account={c.account_name} onconfirm={() => mark('collect', d.id, c)} />
										{/if}
										{#if act.uncollect}
											<ArmButton kind="uncollect" drop={d.name} account={c.account_name} onconfirm={() => mark('uncollect', d.id, c)} />
										{/if}
									</span>
								</span>
							{:else}
								<span class="cell eligible"><span class="st">{t('campaign.no_accounts')}</span></span>
							{/each}
						</div>
					</li>
				{/each}
			</ol>
		{/if}
	</div>
{/if}

<style>
	.scrim {
		position: fixed;
		inset: 0;
		background: rgba(0, 0, 0, 0.3);
		pointer-events: none;
		z-index: 30;
	}
	.sheet {
		position: fixed;
		top: 0;
		right: 0;
		bottom: 0;
		width: min(520px, 100%);
		overflow: auto;
		padding: 20px;
		display: grid;
		align-content: start;
		gap: 10px;
		background: var(--surface-2);
		border-left: 1px solid var(--border);
		color: var(--text);
		z-index: 31;
		box-shadow: -12px 0 32px rgba(0, 0, 0, 0.25);
	}
	.x {
		position: absolute;
		right: 10px;
		top: 10px;
		display: grid;
		place-items: center;
		width: 28px;
		height: 28px;
		border: 0;
		border-radius: 6px;
		background: none;
		color: var(--muted);
		cursor: pointer;
	}
	.x svg {
		width: 12px;
		height: 12px;
	}
	.x:hover {
		background: var(--surface-3);
		color: var(--text);
	}
	.x:focus-visible,
	.btn:focus-visible,
	.link a:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.msg {
		margin: 24px 0 0;
		color: var(--t2);
		font-size: 13px;
		display: flex;
		gap: 8px;
		align-items: center;
		flex-wrap: wrap;
	}
	.msg.err {
		color: var(--red);
	}
	.btn {
		font-size: 12px;
		font-weight: 500;
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: 6px;
		padding: 3px 9px;
		color: var(--text);
		cursor: pointer;
	}
	header {
		display: flex;
		gap: 10px;
		align-items: flex-start;
		padding-right: 32px;
	}
	header :global(.pm) {
		margin-top: 4px;
	}
	h2 {
		margin: 0;
		font-size: 16px;
	}
	header p,
	.sum {
		margin: 2px 0 0;
		color: var(--muted);
		font-size: 13px;
	}
	.sum {
		margin: 0;
	}
	.ended {
		color: var(--amber);
	}
	.link {
		margin: 0;
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		align-items: center;
		padding: 8px 10px;
		border-radius: var(--radius);
		background: var(--amber-soft);
		color: var(--amber);
		font-size: 13px;
	}
	.link a {
		color: inherit;
		font-weight: 600;
		text-decoration: underline;
	}
	ol {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 6px;
	}
	li {
		display: grid;
		grid-template-columns: 46px minmax(0, 1fr) minmax(0, 1.4fr);
		gap: 12px;
		align-items: center;
		padding: 8px;
		border: 1px solid var(--border-soft);
		border-radius: var(--radius);
		background: var(--surface);
	}
	li.hl {
		border-color: var(--accent);
		background: var(--accent-soft);
	}
	.img {
		width: 46px;
		height: 46px;
		border-radius: 6px;
		overflow: hidden;
	}
	.what {
		display: grid;
		gap: 2px;
		min-width: 0;
	}
	.nm {
		font-weight: 600;
		font-size: 14px;
		display: flex;
		gap: 6px;
		align-items: center;
		flex-wrap: wrap;
		overflow-wrap: anywhere;
	}
	.you {
		font-size: 11px;
		font-weight: 600;
		color: var(--accent);
		border: 1px solid var(--accent);
		border-radius: 999px;
		padding: 0 6px;
	}
	.req {
		color: var(--muted);
		font-size: 12px;
		font-family: var(--mono);
	}
	.req.dim {
		color: var(--dim);
		font-family: var(--font);
	}
	/* Cells share columns, so statuses and actions line up within a drop and
	   the action column is only as wide as the drop needs. */
	.cells {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto auto;
		gap: 4px 8px;
	}
	.cell {
		grid-column: 1 / -1;
		display: grid;
		grid-template-columns: subgrid;
		gap: 2px 8px;
		align-items: center;
		font-size: 12px;
	}
	.who {
		display: inline-flex;
		gap: 5px;
		align-items: center;
		min-width: 0;
		color: var(--t2);
	}
	.who span {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.acts {
		display: flex;
		justify-content: flex-end;
		gap: 4px;
	}
	.mark-err {
		margin: 0;
	}
	.st {
		color: var(--muted);
		font-family: var(--mono);
		text-align: right;
	}
	/* An armed Confirm button covers the status; hide it so no text peeks out. */
	.cell:has(:global(.ab.armed)) .st {
		visibility: hidden;
	}
	.claimed .st {
		color: var(--green);
	}
	.blocked .st {
		color: var(--amber);
	}
	.accruing .st,
	.claimable .st {
		color: var(--accent);
	}
	.bar {
		grid-column: 1 / -1;
		grid-row: 2;
		height: 4px;
		border-radius: 2px;
		background: var(--surface-3);
		overflow: hidden;
	}
	.bar i {
		display: block;
		height: 100%;
		background: var(--accent);
	}
	@media (max-width: 640px) {
		.sheet {
			width: 100%;
			border-left: 0;
			padding: 16px;
		}
		li {
			grid-template-columns: 46px minmax(0, 1fr);
		}
		.cells {
			grid-column: 1 / -1;
		}
	}
</style>
