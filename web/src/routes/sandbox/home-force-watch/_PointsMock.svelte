<script lang="ts">
	import Icon from '$lib/components/Icon.svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { pscenes, pstate, pts, type PScene, type PVariant } from './_points';

	let { variant }: { variant: PVariant } = $props();

	let scene = $state<PScene>('watching');
	const st = $derived(pstate(scene));
	const failed = $derived(st.read === 'failed');
	const liveN = $derived(st.live.length + st.more);
	const showC = $derived(variant.id === 'c' && st.collected !== null);
</script>

{#snippet who(watchers: string[])}
	{#if watchers.length}
		<span class="on">{watchers.join(', ')}</span>
	{:else}
		<span class="off">Nobody watching</span>
	{/if}
{/snippet}

<!-- Today's Points watch tile, used by C and by A and B with no Twitch channel. -->
{#snippet todayTile()}
	<a class="kpi" href="/points">
		<div class="label">Points watch</div>
		{#if st.live.length}
			<ul class="tile">
				{#each st.live as r (r.platform + r.channel)}
					<li>
						<span class="dot"></span><PlatformMark platform={r.platform} size="sm" /><b>{r.channel}</b>
						{@render who(r.watchers)}
					</li>
				{/each}
			</ul>
			{#if st.more > 0}<div class="sub more">{st.more} more live</div>{/if}
		{:else}
			<div class="val none-live">None live</div>
			<div class="sub">{st.total} channels waiting</div>
		{/if}
	</a>
{/snippet}

{#snippet tileA()}
	{#if st.read === 'none'}
		{@render todayTile()}
	{:else}
		<a class="kpi" href="/points">
			<div class="label">
				Points watch
				{#if failed}<span class="warn" title="Read failed"><Icon name="alert" size={12} /></span>{/if}
			</div>
			{#if st.points === null}
				<div class="val none-live">Not read yet</div>
			{:else}
				<div class="val" class:stale={failed}>{pts(st.points)}<small>pts</small></div>
			{/if}
			{#if st.live.length}
				{@const r = st.live[0]}
				<div class="line">
					<span class="dot"></span><PlatformMark platform={r.platform} size="sm" />
					{#if r.watchers.length}
						<b>{r.channel}</b><span class="on">{r.watchers.join(', ')}</span>
					{:else}
						<span class="off">{liveN} live · Nobody watching</span>
					{/if}
				</div>
			{:else}
				<div class="sub">None live · {st.total} waiting</div>
			{/if}
			{#if failed}
				<div class="sub bad">Read failed · {st.readAgo}</div>
			{:else if st.live.length && st.live[0].watchers.length && liveN > 1}
				<div class="sub">{liveN - 1} more live</div>
			{/if}
		</a>
	{/if}
{/snippet}

{#snippet tileB()}
	<a class="kpi" href="/points">
		<div class="label">
			<span>Points watch</span>
			{#if st.read !== 'none'}
				<span class="tot" class:stale={failed}>
					{#if failed}<span class="warn"><Icon name="alert" size={12} /></span>{/if}
					{st.points === null ? '–' : pts(st.points)} pts
				</span>
			{/if}
		</div>
		{#if st.live.length}
			<ul class="tile">
				{#each st.live as r (r.platform + r.channel)}
					<li>
						<span class="dot"></span><PlatformMark platform={r.platform} size="sm" /><b>{r.channel}</b>
						<span class="who">{@render who(r.watchers)}</span>
						{#if r.platform === 'twitch'}<span class="bal" class:stale={failed}>{r.balance === null ? '–' : pts(r.balance)}</span>{/if}
					</li>
				{/each}
			</ul>
			{#if failed}
				<div class="sub bad more">Read failed · {st.readAgo}</div>
			{:else if st.read === 'pending'}
				<div class="sub more">Reading balances</div>
			{:else if st.more > 0}
				<div class="sub more">{st.more} more live</div>
			{/if}
		{:else}
			<div class="val none-live">None live</div>
			<div class="sub">{st.total} channels waiting</div>
		{/if}
	</a>
{/snippet}

{#snippet tileC()}
	<a class="kpi" href="/points">
		<div class="label">Points collected</div>
		<div class="val">{pts(st.collected?.all ?? 0)}<small>pts</small></div>
		<div class="sub">{pts(st.collected?.today ?? 0)} today</div>
	</a>
{/snippet}

{#snippet row()}
	<div class="kpis" class:five={showC} class:wide={variant.id === 'b'}>
		<div class="kpi">
			<div class="label">Minutes watched</div>
			<div class="val">12,480<small>min</small></div>
			<div class="sub">310 today</div>
		</div>
		<a class="kpi" href="/inventory">
			<div class="label">Drops claimed</div>
			<div class="val">142</div>
			<div class="sub">3 today</div>
		</a>
		{#if variant.id === 'a'}
			{@render tileA()}
		{:else if variant.id === 'b'}
			{@render tileB()}
		{:else}
			{@render todayTile()}
			{#if showC}{@render tileC()}{/if}
		{/if}
		<a class="kpi" href="/settings#accounts">
			<div class="label">Accounts mining</div>
			<div class="val">{st.mining}<small>/ 4</small></div>
			<div class="sub">4 accounts healthy</div>
		</a>
	</div>
{/snippet}

<div class="scenes" role="group" aria-label="Scene">
	{#each pscenes as s (s.id)}
		<button type="button" class:sel={scene === s.id} aria-pressed={scene === s.id} onclick={() => (scene = s.id)}>{s.label}</button>
	{/each}
</div>

<div class="frame desk">
	<div class="head">
		<h1>Home</h1>
		<span class="now"><span class="dot g"></span>Mining · {st.mining} of 4 accounts</span>
	</div>
	{@render row()}
	<div class="ph" aria-hidden="true"></div>
</div>

<p class="cap">Phone, 390px</p>
<div class="frame phone">
	<div class="head">
		<h1>Home</h1>
		<span class="now"><span class="dot g"></span>Mining · {st.mining} of 4 accounts</span>
	</div>
	{@render row()}
	<div class="ph" aria-hidden="true"></div>
</div>

<style>
	.scenes {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		margin-bottom: 10px;
	}
	.scenes button {
		font: inherit;
		font-size: 12px;
		padding: 4px 10px;
		border-radius: 999px;
		border: 1px solid var(--border);
		background: var(--surface-2);
		color: var(--t2);
		cursor: pointer;
	}
	.scenes .sel {
		border-color: var(--accent);
		background: var(--accent-soft);
		color: var(--text);
	}
	.cap {
		margin: 20px 0 6px;
		font-size: 12px;
		color: #b5b5bb;
	}
	.frame {
		padding: 24px;
		border: 1px solid var(--border);
		border-radius: 12px;
		background: var(--bg);
		color: var(--text);
		container-type: inline-size;
	}
	.frame.phone {
		width: 390px;
		max-width: 100%;
		box-sizing: border-box;
		padding: 16px;
	}
	.head {
		display: flex;
		align-items: center;
		gap: 8px 16px;
		flex-wrap: wrap;
		margin-bottom: 24px;
	}
	h1 {
		font-size: 18px;
		font-weight: 600;
		margin: 0;
	}
	.now {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		font-size: 13px;
		font-weight: 500;
		color: var(--green);
	}
	.dot.g {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: currentColor;
	}
	.ph {
		height: 120px;
		border-radius: 10px;
		background: var(--surface);
		border: 1px solid var(--border-soft);
	}

	/* Copied from the real Home KPI row, media queries turned into container queries. */
	.kpis {
		display: grid;
		grid-template-columns: repeat(4, minmax(0, 1fr));
		border: 1px solid var(--border-soft);
		border-radius: var(--radius);
		background: var(--surface);
		margin-bottom: 28px;
	}
	.kpis.five {
		grid-template-columns: repeat(5, minmax(0, 1fr));
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
	a.kpi:hover .label {
		color: var(--text);
	}
	a.kpi:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
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
	.kpi .val.none-live {
		font-size: 20px;
		color: var(--t2);
	}
	.kpi .sub {
		font-size: 12px;
		color: var(--t3);
		margin-top: 2px;
	}
	.kpi .sub.more {
		margin-top: 6px;
	}
	.kpi .sub.bad {
		color: var(--amber);
	}
	.stale {
		opacity: 0.6;
	}
	.warn {
		display: inline-flex;
		color: var(--amber);
	}
	.tile {
		list-style: none;
		margin: 8px 0 0;
		padding: 0;
		display: grid;
		gap: 6px;
	}
	.tile li,
	.line {
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: 13px;
		min-width: 0;
	}
	.line {
		margin-top: 4px;
		font-size: 12px;
	}
	.tile b,
	.line b {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		min-width: 0;
	}
	.line b {
		font-weight: 500;
	}
	.tile li > :last-child,
	.line > :last-child {
		margin-left: auto;
		font-size: 12px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		min-width: 0;
	}
	.line > :last-child {
		margin-left: 0;
	}
	.dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--red);
		flex: none;
	}
	.on {
		color: var(--green);
		font-weight: 500;
	}
	.off {
		color: var(--t3);
	}

	/* B: total beside the label, balance at the end of each row. */
	.tot {
		margin-left: auto;
		display: inline-flex;
		align-items: center;
		gap: 4px;
		color: var(--text);
		font-weight: 500;
		font-variant-numeric: tabular-nums;
		white-space: nowrap;
	}
	.who {
		margin-left: auto;
		font-size: 12px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		min-width: 0;
	}
	.tile li > .bal {
		flex: none;
		margin-left: 6px;
		color: var(--t2);
		font-variant-numeric: tabular-nums;
		overflow: visible;
	}
	.tile li > .bal.stale {
		opacity: 0.6;
	}

	@container (max-width: 760px) {
		.kpis,
		.kpis.five {
			grid-template-columns: 1fr 1fr;
		}
		.kpi {
			padding: 14px 16px;
		}
		.kpi .val {
			font-size: 24px;
		}
		.kpi:nth-child(n + 3) {
			border-top: 1px solid var(--border-soft);
		}
		.kpi:nth-child(odd) {
			border-left: 0;
		}
		.kpis.five .kpi:nth-child(5) {
			grid-column: 1 / -1;
		}
		/* B: the points tile and Accounts mining each take a full row on phones. */
		.kpis.wide .kpi:nth-child(3),
		.kpis.wide .kpi:nth-child(4) {
			grid-column: 1 / -1;
			border-left: 0;
		}
	}
</style>
