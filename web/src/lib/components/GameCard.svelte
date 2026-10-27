<script lang="ts">
	import { etaShort, formatInt } from '$lib/format';
	import { cardCampaign, foldRows, type AccountRow, type GameCard } from '$lib/home';
	import { t, tn } from '$lib/i18n/index.svelte';
	import { foldQueue } from '$lib/upnext';
	import { scanStamp } from '$lib/scan';
	import { linkNames } from '$lib/games';
	import BoxArt from './BoxArt.svelte';
	import type { CampaignTarget } from './CampaignDrawer.svelte';
	import Icon from './Icon.svelte';
	import LinkRecheck from './LinkRecheck.svelte';
	import PlatformMark from './PlatformMark.svelte';

	let {
		card,
		lastScan,
		nextScan = null,
		now,
		readyAccounts = null,
		over = false,
		onopen
	}: {
		card: GameCard;
		lastScan: string | null;
		nextScan?: string | null;
		now: number;
		readyAccounts?: number | null;
		/** Past what fits: laid out off the flow so it can still be measured. */
		over?: boolean;
		/** Opens the campaign drawer; without it the campaign name is plain text. */
		onopen?: (t: CampaignTarget) => void;
	} = $props();

	let open = $state(false);
	const fold = $derived(foldRows(card.rows));
	const folded = $derived(card.rows.length > 3);
	const list = $derived(folded && !open ? fold.shown : card.rows);
	const game = $derived(card.game);
	const unlinked = $derived(linkNames(game));
	const camp = $derived(cardCampaign(card));
	const wait = $derived(card.waitFor);
	const stateText = $derived(
		game.state === 'mining'
			? t('home.state_mining')
			: game.state === 'waiting'
				? waitBadge()
				: game.state === 'done'
					? t('home.state_done')
					: t('home.state_none')
	);
	const waitLine = $derived(lineFor());

	function waitBadge(): string {
		if (wait?.kind === 'account') return t('home.wait_account', { account: wait.account });
		if (wait?.kind !== 'live') return t('home.state_waiting');
		if (wait.channels.length === 0) return t('home.wait_live_any');
		const n = wait.channels.length - 1 + wait.more;
		return n > 0
			? t('home.wait_live_more', { channel: wait.channels[0], n })
			: t('home.wait_live', { channel: wait.channels[0] });
	}

	function lineFor(): string {
		if (wait?.kind === 'account') return t('home.waiting_line', { account: wait.account });
		if (wait?.kind !== 'live') return t('home.waiting_line_any');
		if (wait.channels.length === 0) return t('home.live_line_any');
		if (wait.channels.length === 1 && wait.more === 0) return t('home.live_line', { channel: wait.channels[0] });
		const names = wait.channels.join(', ') + (wait.more > 0 ? ` +${wait.more}` : '');
		return t('home.live_line_some', { channels: names });
	}
	const foot = $derived(
		[
			folded ? t('home.most_advanced') : '',
			card.moreInUpNext > 0 ? tn('home.more_in_upnext', card.moreInUpNext, { game: game.name }) : ''
		]
			.filter(Boolean)
			.join(' · ')
	);
	const queuedFold = $derived(foldQueue(card.queued, false, 3));
	const showRows = $derived(game.state === 'mining' && card.rows.length > 0);

	function pct(r: AccountRow): string {
		return `${Math.min(100, Math.max(0, r.progress * 100)).toFixed(1)}%`;
	}
	function left(r: AccountRow): string {
		const c = r.runner.current;
		return c ? etaShort(c.required_minutes - c.minutes) : '';
	}
</script>

<article class="game p{card.pos <= 2 ? card.pos : 3}" class:over aria-hidden={over || undefined} inert={over}>
	<div class="artw">
		<BoxArt src={game.box_art_url} name={game.name} size={card.size} />
	</div>
	<div class="body">
		<div class="status">
			<h3 class="gname">{game.name}</h3>
			<span class="state {game.state}">{stateText}</span>
			{#if game.state === 'no_campaign'}
				<span class="camp">{scanStamp(lastScan, nextScan, new Date(now)).map((x) => t(x.key, x.vars)).join(' · ')}</span>
			{:else if camp?.id && onopen}
				<button
					type="button"
					class="camp clink"
					data-campaign
					aria-haspopup="dialog"
					title={t('campaign.open')}
					onclick={() => onopen({ platform: camp.platform, id: camp.id })}>{camp.name}</button
				>
			{:else if camp?.name}
				<span class="camp">{camp.name}</span>
			{/if}
		</div>

		{#if unlinked.length > 0}
			<p class="link-l">
				<Icon name="alert" size={13} /><span>{t('home.needs_link', { accounts: unlinked.join(', ') })}</span>
				<LinkRecheck accountIds={game.needs_link.map((n) => n.account_id)} />
			</p>
		{/if}

		{#if readyAccounts !== null && card.pos === 1}
			<p class="idle-l">{tn('home.ready_all', readyAccounts)}</p>
		{/if}

		{#if showRows}
			{#if folded}
				<div class="summ">
					<span><b>{t('home.summary_mining', { m: card.rows.length })}</b></span>
					<button type="button" class="toggle" aria-expanded={open} onclick={() => (open = !open)}>
						<span>{open ? t('home.show_fewer') : t('home.show_all', { n: card.rows.length })}</span>
						<svg viewBox="0 0 12 12" aria-hidden="true"><path d="M3 4.5 6 7.5l3-3" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
					</button>
				</div>
			{/if}
			<div class="accts">
				{#each list as r (r.runner.account_id)}
					{@const c = r.runner.current}
					<div class="acct">
						<div class="who"><PlatformMark platform={r.runner.platform} /><b>{r.runner.account_name}</b></div>
						{#if c}
							<div class="drop">
								<div class="l">
									<span>{c.benefit_name}{#if r.runner.channel} <span class="ch">· {r.runner.channel}</span>{/if}</span>
									<span>{Math.floor(c.minutes)}/{c.required_minutes}</span>
								</div>
								<div class="bar"><i style:width={pct(r)}></i></div>
							</div>
							<div class="eta"><span>{left(r)}</span><small>{t('home.left')}</small></div>
						{/if}
					</div>
				{/each}
			</div>
			{#if folded || card.moreInUpNext > 0}
				<div class="foot">{foot}</div>
			{/if}
		{:else if game.state === 'waiting' && card.queued.length > 0}
			<ul class="queued">
				<li class="q-h">{t('home.queued_head', { line: waitLine })}</li>
				{#each queuedFold.rows as q (q.benefit_id)}
					<li><span>{q.benefit_name}</span><span>{t('home.queued_min', { n: formatInt(q.required_minutes) })}</span></li>
				{/each}
				{#if queuedFold.more > 0}
					<li class="q-more">{tn('home.more_in_upnext', queuedFold.more, { game: game.name })}</li>
				{/if}
			</ul>
		{:else if game.state === 'waiting'}
			<p class="idle-l">{waitLine}</p>
		{:else if game.state === 'done'}
			<p class="idle-l">{t('home.done_line')}</p>
		{/if}
	</div>
</article>

<style>
	.game {
		display: grid;
		grid-template-columns: var(--aw) minmax(0, 1fr);
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		overflow: hidden;
	}
	.game.over {
		position: absolute;
		left: 0;
		right: 0;
		visibility: hidden;
		pointer-events: none;
	}
	:global(.game + .game) {
		margin-top: 10px;
	}
	.game.p2 {
		margin-top: 12px;
	}
	.artw {
		position: relative;
		overflow: hidden;
		isolation: isolate;
		display: grid;
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
	.body {
		padding: 16px 20px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		justify-content: center;
	}
	.status {
		display: flex;
		align-items: center;
		gap: 6px 8px;
		flex-wrap: wrap;
		min-width: 0;
	}
	.gname {
		margin: 0 4px 0 0;
		font-size: 15px;
		font-weight: 600;
		letter-spacing: -0.01em;
		white-space: nowrap;
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
	.camp {
		color: var(--t2);
		font-size: 13px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		min-width: 0;
	}
	.clink {
		font-family: inherit;
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
		color: var(--text);
		text-decoration-style: solid;
		text-decoration-color: currentColor;
	}
	.clink:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
		border-radius: 3px;
	}
	.p1 {
		--aw: 168px;
	}
	.p1 .artw {
		min-height: 200px;
	}
	.p1 .body {
		padding: 18px 20px;
		justify-content: flex-start;
	}
	.p1 .gname {
		font-size: 18px;
	}
	.p2 {
		--aw: 168px;
	}
	.p2 .artw {
		min-height: 150px;
	}
	.p2 .body {
		padding: 18px 20px;
	}
	.p2 .gname {
		font-size: 18px;
	}
	.p3 {
		--aw: 80px;
	}
	.p3 .artw {
		min-height: 64px;
	}
	.p3 .body {
		padding: 12px 16px;
	}
	.p3 .gname {
		font-size: 14px;
	}
	.accts {
		margin-top: 14px;
		display: grid;
		gap: 12px;
	}
	.acct {
		display: grid;
		grid-template-columns: 136px minmax(0, 1fr) 56px;
		gap: 14px;
		align-items: center;
	}
	.who {
		display: flex;
		align-items: center;
		gap: 7px;
		min-width: 0;
	}
	.who b {
		font-weight: 500;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.drop {
		min-width: 0;
	}
	.drop .l {
		display: flex;
		justify-content: space-between;
		gap: 8px;
		font-size: 13px;
	}
	.drop .l > span:first-child {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.drop .l > span:last-child {
		color: var(--t2);
		font: 12px var(--mono);
		white-space: nowrap;
	}
	.bar {
		height: 5px;
		border-radius: 3px;
		background: var(--surface-3);
		margin-top: 6px;
		overflow: hidden;
	}
	.bar i {
		display: block;
		height: 100%;
		border-radius: 3px;
		background: var(--accent);
		transition: width 0.8s ease;
	}
	.eta {
		text-align: right;
		font: 500 13px var(--mono);
	}
	.eta small {
		display: block;
		color: var(--t3);
		font: 12px var(--font);
	}
	.ch {
		margin-left: 4px;
		color: var(--t3);
		font-size: 12px;
	}
	.foot {
		margin-top: auto;
		padding-top: 14px;
		color: var(--t3);
		font-size: 12px;
	}
	.queued {
		margin: 8px 0 0;
		padding: 0;
		list-style: none;
		display: grid;
		gap: 2px;
		font-size: 13px;
	}
	.queued li {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: 12px;
		min-width: 0;
	}
	.queued li span:first-child {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.queued li span:last-child {
		color: var(--t3);
		font: 12px var(--mono);
		white-space: nowrap;
	}
	.queued .q-more {
		color: var(--t3);
		font-size: 12px;
	}
	.queued .q-h {
		color: var(--t2);
		font-size: 12px;
	}
	.link-l {
		margin: 8px 0 0;
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 4px 6px;
		color: var(--amber);
		font-size: 12px;
	}
	.idle-l {
		margin: 10px 0 0;
		color: var(--t2);
		font-size: 13px;
	}
	.summ {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		margin-top: 12px;
		padding-bottom: 10px;
		border-bottom: 1px solid var(--border-soft);
		font-size: 13px;
		color: var(--t2);
	}
	.summ b {
		color: var(--text);
		font-weight: 500;
	}
	.toggle {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		background: none;
		border: 1px solid var(--border);
		border-radius: 6px;
		padding: 2px 8px;
		font-size: 12px;
		color: var(--t2);
		white-space: nowrap;
		cursor: pointer;
	}
	.toggle:hover {
		color: var(--text);
		border-color: var(--t2);
	}
	.toggle:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.toggle svg {
		width: 12px;
		height: 12px;
		transition: transform 0.15s ease;
	}
	.toggle[aria-expanded='true'] svg {
		transform: rotate(180deg);
	}
	@media (max-width: 720px) {
		.p1,
		.p2 {
			--aw: 84px;
		}
		.p3 {
			--aw: 52px;
		}
		.p1 .artw {
			min-height: 0;
		}
		.p1 .body,
		.p2 .body {
			padding: 14px 16px;
		}
		.p3 .body {
			padding: 10px 14px;
		}
		.camp {
			white-space: normal;
		}
		.acct {
			grid-template-columns: minmax(0, 1fr) auto;
			gap: 6px 12px;
		}
		.drop {
			grid-column: 1 / -1;
			grid-row: 2;
		}
		.eta small {
			display: inline;
			margin-left: 4px;
		}
	}
</style>
