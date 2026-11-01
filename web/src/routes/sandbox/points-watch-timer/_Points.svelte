<script lang="ts">
	import type { Platform, PointsMode } from '$lib/api/types';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import PointsModePicker from '$lib/components/PointsMode.svelte';
	import { pointsSupported } from '$lib/accounts';
	import { uptime } from '$lib/format';
	import { freshLine } from '$lib/points';
	import Pill from './_Pill.svelte';
	import { pts, type Acc, type Row, type Variant } from './_data';

	/** The Points page as live today, with the timer where the variant puts it. */
	let {
		variant,
		accs,
		order,
		now,
		start,
		readAt,
		onmode
	}: {
		variant: Variant;
		accs: Acc[];
		order: Row[];
		now: number;
		start: (x: Acc) => string;
		readAt: (x: Acc) => string;
		onmode: (id: string, m: PointsMode) => void;
	} = $props();

	const LABEL: Record<Platform, string> = { twitch: 'Twitch', kick: 'Kick' };
	const dash = $derived(variant.id === 'd');
	let tab = $state<Platform>('twitch');

	const watching = $derived(accs.filter((x) => x.channel !== null && x.a.enabled));
	const shownAccs = $derived(accs.filter((x) => x.a.platform === tab));
	const rows = $derived(order.filter((r) => r.platform === tab));
	const byId = $derived(new Map(accs.map((x) => [x.a.id, x])));
	const time = (x: Acc) => uptime(start(x), now);
	/** D collapses the header past two watchers. */
	const collapsed = $derived(dash && watching.length > 2);
</script>

<div class="head">
	<h1>Points</h1>
	{#if watching.length === 0}
		<span class="now grey"><span class="dot"></span>Nobody on Points watch</span>
	{:else if collapsed}
		<span class="now"><span class="dot"></span>Points watching · {watching.length} accounts<Pill /></span>
	{:else}
		<span class="now"
			><span class="dot"></span>
			<span
				>Points watching · {#each watching as x, i (x.a.id)}{i ? ', ' : ''}{x.a.name} on {x.channel}{#if variant.head}<span
							class="tm">{time(x)}</span
						>{/if}{/each}</span
			>{#if variant.head}<Pill />{/if}</span
		>
	{/if}
	<span class="stamp">Balances refresh every 60 min</span>
</div>

<div class="tabs" role="tablist" aria-label="Platform">
	{#each ['twitch', 'kick'] as const as p (p)}
		<button type="button" role="tab" aria-selected={tab === p} onclick={() => (tab = p)}>
			<PlatformMark platform={p} />{LABEL[p]}<span class="n">{order.filter((r) => r.platform === p).length}</span>
		</button>
	{/each}
</div>

<p class="hint">Watched when no drop is live, always for Points only accounts. Top live channel first.</p>

{#if dash && shownAccs.length}<h2 class="rh">Accounts<Pill /></h2>{/if}
<section class="strip" class:dash aria-label="Accounts">
	{#each shownAccs as x (x.a.id)}
		{@const on = x.channel !== null && x.a.enabled}
		<div class="acc" class:paused={!x.a.enabled} class:on={dash && on}>
			{#if dash}
				<div class="top">
					<b class="pf-name {x.a.platform}">{x.a.name}</b>
					{#if x.points !== null}
						<span class="sum" class:stale={x.stale}>{pts(x.points)}<small>pts</small></span>
					{/if}
				</div>
				<div class="state" class:on>
					<span class="sd"></span>
					{#if on}
						<span class="txt">Watching <b>{x.channel}</b> · <span class="tm">{time(x)}</span></span>
					{:else}
						<span class="txt">{x.line}</span>
					{/if}
				</div>
			{:else}
				<div class="top">
					<b class="pf-name {x.a.platform}">{x.a.name}</b>
					{#if on && variant.card}
						<small>Watching {x.channel} · <span class="tm">{time(x)}</span><Pill /></small>
					{:else if on}
						<small>Points watching {x.channel}</small>
					{:else}
						<small>{x.line}</small>
					{/if}
				</div>
			{/if}
			{#if pointsSupported(x.a)}
				<PointsModePicker a={x.a} onpick={(m) => onmode(x.a.id, m)} />
				<div class="tot">
					<small class:fresh-stale={x.stale}>{freshLine({ kind: 'ok', at: readAt(x), stale: !!x.stale }, now)}</small>
				</div>
			{:else}
				<div class="tot"><small>{LABEL[x.a.platform]} balances and bonus claims coming soon</small></div>
			{/if}
		</div>
	{/each}
</section>

<h2 class="rh">Points watch order</h2>
<ol>
	{#each rows as c, i (c.channel)}
		{@const nowAccs = c.live
			? c.ticks.map((tk) => byId.get(tk.id)).filter((x): x is Acc => !!x && x.channel === c.channel && x.a.enabled)
			: []}
		<li>
			<div class="mid">
				<span class="ch">
					<span class="rk">{String(i + 1).padStart(2, '0')}</span>
					<PlatformMark platform={c.platform} />
					<span class="who">
						<b
							><span class="nm">{c.channel}</span><span class="ldot" class:live={c.live}></span
							>{#if variant.order && nowAccs.length}<Pill />{/if}</b
						>
						<small>
							{#if nowAccs.length}<span class="on"
									>Watching now · {#each nowAccs as x, j (x.a.id)}{j ? ', ' : ''}{x.a.name}{#if variant.order}<span
												class="tm">{time(x)}</span
											>{/if}{/each}</span
								>
							{:else if c.live}Live, nobody ticked{:else}Offline{/if}
						</small>
					</span>
				</span>
				<div class="chips">
					{#each c.ticks as tk (tk.id)}
						{@const x = byId.get(tk.id)}
						<span class="chip on" class:skip={x && (!x.a.enabled || x.a.points_mode === 'off')}>
							{#if x && nowAccs.includes(x)}<span class="live"></span>{/if}
							<span class="nm">{x?.a.name ?? tk.id}</span>
							{#if tk.bal !== null}<span class="bal">{pts(tk.bal)}</span>{/if}
						</span>
					{/each}
				</div>
			</div>
		</li>
	{/each}
</ol>

<style>
	/* Head: copied from PageHead. */
	.head {
		display: flex;
		align-items: center;
		gap: 8px 16px;
		margin-bottom: 24px;
		flex-wrap: wrap;
	}
	h1 {
		font-size: 18px;
		font-weight: 600;
		margin: 0;
		letter-spacing: -0.01em;
	}
	.now {
		display: inline-flex;
		align-items: baseline;
		gap: 8px;
		font-size: 13px;
		font-weight: 500;
		color: var(--green);
		min-width: 0;
	}
	.now.grey {
		color: var(--t2);
	}
	.now .dot {
		align-self: flex-start;
		margin-top: 0.45em;
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: currentColor;
		flex: none;
	}
	.now .tm {
		margin-left: 5px;
		color: var(--t2);
		font-weight: 500;
	}
	.stamp {
		margin-left: auto;
		color: var(--t3);
		font-size: 12px;
		font-family: var(--mono);
	}
	/* The new part, everywhere: time on the channel, tabular so it does not jump. */
	.tm {
		font-variant-numeric: tabular-nums;
		white-space: nowrap;
	}
	.tabs {
		display: flex;
		gap: 4px;
		border-bottom: 1px solid var(--border-soft);
		margin-bottom: 12px;
	}
	.tabs button {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		font: inherit;
		font-size: 13.5px;
		font-weight: 500;
		padding: 8px 12px;
		margin-bottom: -1px;
		border: 0;
		border-bottom: 2px solid transparent;
		background: none;
		color: var(--t2);
		cursor: pointer;
	}
	.tabs button:hover,
	.tabs button[aria-selected='true'] {
		color: var(--text);
	}
	.tabs button[aria-selected='true'] {
		border-bottom-color: var(--accent);
	}
	.tabs button:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.n {
		font: 500 11px/1 var(--mono);
		padding: 3px 6px;
		border-radius: 999px;
		background: var(--surface-2);
		color: var(--t3);
	}
	.hint {
		margin: 0 0 16px;
		font-size: 12.5px;
		color: var(--t3);
		max-width: 80ch;
	}
	.strip {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(min(300px, 100%), 1fr));
		gap: 8px;
		margin-bottom: 20px;
	}
	.acc {
		display: grid;
		gap: 8px;
		padding: 10px 12px;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		align-content: start;
		min-width: 0;
	}
	.top {
		display: flex;
		align-items: baseline;
		flex-wrap: wrap;
		gap: 4px 8px;
		min-width: 0;
	}
	.top b {
		font-size: 13.5px;
	}
	.top small {
		color: var(--t3);
		font-size: 12px;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.top small .tm {
		color: var(--text);
		font-weight: 500;
	}
	.tot {
		display: flex;
		font-size: 12px;
		color: var(--t3);
	}
	.tot small {
		font-size: 11.5px;
	}
	.fresh-stale {
		color: var(--amber);
	}
	.paused {
		opacity: 0.55;
	}

	/* D: the account card as a small dashboard. Watching cards get a green tint. */
	.strip.dash {
		gap: 10px;
	}
	.dash .acc {
		gap: 10px;
		padding: 12px 14px;
	}
	.dash .acc.on {
		background: color-mix(in srgb, var(--green) 6%, var(--surface));
		border-color: color-mix(in srgb, var(--green) 22%, var(--border-soft));
	}
	.dash .top {
		flex-wrap: nowrap;
		justify-content: space-between;
	}
	.dash .top b {
		font-size: 14px;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.sum {
		flex: none;
		font-size: 17px;
		font-weight: 600;
		letter-spacing: -0.02em;
		font-variant-numeric: tabular-nums;
		color: var(--text);
	}
	.sum small {
		margin-left: 3px;
		font-size: 12px;
		font-weight: 500;
		letter-spacing: 0;
		color: var(--t2);
	}
	.sum.stale {
		color: var(--t2);
	}
	.state {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 13px;
		color: var(--t2);
		min-width: 0;
	}
	.state .txt {
		min-width: 0;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.state .sd {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--t3);
		flex: none;
	}
	.state.on {
		color: var(--text);
	}
	.state.on .sd {
		background: var(--green);
		box-shadow: 0 0 0 3px var(--green-soft);
	}
	.state b {
		font-weight: 600;
	}
	.state .tm {
		color: var(--green);
		font-weight: 600;
	}

	.rh {
		display: flex;
		align-items: center;
		margin: 0 0 8px;
		font-size: 11px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--t3);
	}
	.rh :global(.new) {
		text-transform: none;
		letter-spacing: 0.02em;
	}
	ol {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 6px;
	}
	li {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 10px 12px;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		min-width: 0;
	}
	.mid {
		flex: 1;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: 8px 16px;
		flex-wrap: wrap;
	}
	.ch {
		display: flex;
		align-items: center;
		gap: 10px;
		min-width: 0;
		flex: 0 1 260px;
	}
	.rk {
		font: 500 12px/1 var(--mono);
		color: var(--t3);
		flex: none;
	}
	.who {
		display: grid;
		min-width: 0;
	}
	.who b {
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: 14px;
		font-weight: 600;
		min-width: 0;
	}
	.nm {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.ldot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--surface-3);
		flex: none;
	}
	.ldot.live {
		background: var(--red);
	}
	.who small {
		display: flex;
		align-items: center;
		font-size: 12px;
		color: var(--t3);
		white-space: nowrap;
		min-width: 0;
	}
	.who .on {
		color: var(--green);
		font-weight: 600;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.who .on .tm {
		margin-left: 5px;
		color: var(--t2);
		font-weight: 500;
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		flex: 1;
		min-width: 0;
	}
	.chip {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font-size: 12px;
		height: 26px;
		max-width: 100%;
		padding: 0 10px;
		border-radius: 999px;
		border: 1px solid var(--border);
		background: var(--surface-2);
		color: var(--t3);
	}
	.chip.on {
		border-color: var(--accent);
		background: var(--accent-soft);
		color: var(--text);
	}
	.chip.skip {
		border-style: dashed;
	}
	.chip .bal {
		font-family: var(--mono);
		font-size: 11.5px;
		color: var(--t2);
	}
	.live {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--green);
		box-shadow: 0 0 0 3px var(--green-soft);
		flex: none;
	}
	@media (max-width: 640px) {
		.ch {
			flex-basis: 100%;
		}
		.stamp {
			margin-left: 0;
		}
	}
</style>
