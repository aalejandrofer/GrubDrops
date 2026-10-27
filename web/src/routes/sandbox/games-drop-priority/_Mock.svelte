<script lang="ts">
	import BoxArt from '$lib/components/BoxArt.svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { byEnding, games, stateText, type Variant } from './_variants';

	let { variant }: { variant: Variant } = $props();

	let mode = $state<'ordered' | 'ending_soonest'>('ordered');
	let view = $state<'cards' | 'list'>('cards');
	const ending = $derived(mode === 'ending_soonest');
	const rows = $derived(ending ? byEnding(games) : games);
	const id = $derived(`pm-${variant.id}`);
</script>

{#snippet modeSeg()}
	<div class="seg" role="group" aria-labelledby={id}>
		<button type="button" aria-pressed={mode === 'ordered'} onclick={() => (mode = 'ordered')}>Priority Order</button>
		<button type="button" aria-pressed={mode === 'ending_soonest'} onclick={() => (mode = 'ending_soonest')}>Ending Soonest</button>
	</div>
{/snippet}

{#snippet viewSeg()}
	<div class="seg" role="group" aria-label="View">
		<button type="button" aria-pressed={view === 'cards'} onclick={() => (view = 'cards')}>Cards</button>
		<button type="button" aria-pressed={view === 'list'} onclick={() => (view = 'list')}>List</button>
	</div>
{/snippet}

{#snippet modeGroup()}
	<span class="mode-label" {id}>Drop priority</span>
	{@render modeSeg()}
{/snippet}

{#snippet railHint()}
	<p class="rail-hint">{ending ? 'Mine the campaign that ends first, across all your games.' : 'Mine games in the order of your game list.'}</p>
{/snippet}

{#snippet grip(i: number)}
	{#if ending}
		<div class="grip locked" title="Ending Soonest">
			<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" /></svg>
		</div>
	{:else}
		<div class="grip">
			<button type="button" aria-label="Move up" disabled={i === 0}>
				<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 15 6-6 6 6" /></svg>
			</button>
			<span class="h">
				<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M9 6h.01M15 6h.01M9 12h.01M15 12h.01M9 18h.01M15 18h.01" stroke-width="3" /></svg>
			</span>
			<button type="button" aria-label="Move down" disabled={i === rows.length - 1}>
				<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg>
			</button>
		</div>
	{/if}
{/snippet}

{#snippet hint()}
	<p class="hint">{ending ? 'Campaign that ends first mines first.' : 'Top game mines first.'}</p>
{/snippet}

<div class="frame">
	<div class="head">
		<div class="head-l">
			<h1>Games</h1>
			{#if variant.id === 'c'}
				<div class="page-mode">{@render modeGroup()}</div>
			{:else}
				<span class="now"><span class="dot"></span>5 games · last scan 3m ago</span>
			{/if}
		</div>
		<div class="head-r">
			{#if variant.id === 'f'}{@render viewSeg()}{/if}
			<button type="button" class="btn">Scan now</button>
		</div>
	</div>

	<div class="cols">
		<div class="side">
			{#if variant.id === 'f'}
				<aside class="rail" aria-labelledby={id}>
					<h2 {id} class="rh">Priority Mode</h2>
					<div class="wide rseg">{@render modeSeg()}</div>
				</aside>
				<aside class="rail" aria-labelledby="lists-h">
					<h2 id="lists-h" class="rh">Lists</h2>
					<div class="ri">
						<span class="mk" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M4 6h16M4 12h16M4 18h10" /></svg></span>
						<span class="who"><b>Shared list</b><small>5 games · 3 accounts</small></span>
					</div>
					<h3 class="rh">Accounts</h3>
					<ul class="rl">
						{#each [['twitch', 'ryuzec'], ['twitch', 'Phluses'], ['kick', 'TTik3r']] as [pf, nm] (nm)}
							<li class="ri">
								<PlatformMark platform={pf as 'twitch' | 'kick'} />
								<span class="who"><b>{nm}</b><small>Shared list</small></span>
								<button type="button" class="sw" role="switch" aria-checked="false" aria-label={`Own list for ${nm}`}></button>
							</li>
						{/each}
					</ul>
				</aside>
				<aside class="rail" aria-labelledby="fw-h">
					<div class="fw-head">
						<h2 class="rh" id="fw-h">Force watch</h2>
						<button type="button" class="plus" aria-label="Add channel" title="Add channel">+</button>
					</div>
					<p class="fw-hint">Watched when no drops.</p>
					<p class="fw-empty">No channels yet.</p>
				</aside>
			{:else}
			{#if variant.id === 'd'}
				<section class="panel" aria-labelledby={id}>
					<h2 {id}>Drop priority</h2>
					<div class="wide">{@render modeSeg()}</div>
					{@render railHint()}
				</section>
			{/if}
			<section class="panel">
				{#if variant.id === 'e'}
					<div class="e-mode" role="group" aria-label="Drop priority">
						<div class="compact">{@render modeSeg()}</div>
						{#if variant.id === 'e'}{@render railHint()}{/if}
					</div>
				{/if}
				<h2>Account lists</h2>
				<ul>
					<li><PlatformMark platform="twitch" /> ryuzec <small>5 games</small></li>
					<li><PlatformMark platform="twitch" /> Phluses <small>2 games</small></li>
					<li><PlatformMark platform="kick" /> TTik3r <small>2 games</small></li>
				</ul>
			</section>
			<section class="panel">
				<h2>Force watch</h2>
				<p class="muted">No channel queued.</p>
			</section>
			{/if}
		</div>
		<div class="main">
			<label class="search" class:tight={variant.id === 'd' || variant.id === 'e' || variant.id === 'f'}>
				<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg>
				<input type="search" placeholder="Add a game" aria-label="Add a game" />
			</label>

			{#if variant.id === 'a'}
				<div class="mode-row top">{@render modeGroup()}</div>
			{/if}

			{#if variant.id === 'f'}
				<!-- no list header -->
			{:else if variant.id === 'b'}
				<div class="b-head">
					<div class="b-l">
						<div class="mode-row">{@render modeGroup()}</div>
						{@render hint()}
					</div>
					{@render viewSeg()}
				</div>
			{:else}
				<div class="list-head">
					{#if variant.id !== 'd' && variant.id !== 'e'}<h2>Priority</h2>{/if}
					{@render hint()}
					{@render viewSeg()}
				</div>
			{/if}

			{#if variant.id === 'now'}
				<div class="mode-row now-row">{@render modeGroup()}</div>
			{/if}

			<div class="list" class:compact={view === 'list'}>
				{#each rows as g, i (g.id)}
					{#if view === 'list'}
						<div class="game row">
							<span class="rk">{String(i + 1).padStart(2, '0')}</span>
							{@render grip(i)}
							<span class="rname">{g.name}</span>
						</div>
					{:else}
						<article class="game">
							{@render grip(i)}
							<div class="artw">
								<BoxArt src={g.art} name={g.name} size="lg" />
								<span class="rank">{String(i + 1).padStart(2, '0')}</span>
							</div>
							<div class="body">
								<div class="status">
									<h3 class="gname">{g.name}</h3>
									<span class="state {g.state}">{stateText[g.state]}</span>
									<button type="button" class="rm">Remove</button>
								</div>
								{#if g.camps.length}
									<ul class="camps">
										{#each g.camps as c (c.name)}
											<li class="camp">
												<PlatformMark platform={c.platform} /><span>{c.name}</span><small>{c.line}</small>
											</li>
										{/each}
									</ul>
								{:else}
									<p class="muted">Nothing live right now.</p>
								{/if}
							</div>
						</article>
					{/if}
				{/each}
			</div>
		</div>
	</div>
</div>

<style>
	.frame {
		padding: 24px;
		border: 1px solid var(--border);
		border-radius: 12px;
		background: var(--bg);
		color: var(--text);
		overflow: hidden;
	}
	.head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		margin-bottom: 24px;
		flex-wrap: wrap;
	}
	.head-l {
		display: flex;
		align-items: center;
		gap: 8px 16px;
		flex-wrap: wrap;
		min-width: 0;
	}
	h1 {
		font-size: 18px;
		font-weight: 600;
		margin: 0;
		letter-spacing: -0.01em;
	}
	.now {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		font-size: 13px;
		font-weight: 500;
		color: var(--green);
	}
	.dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: currentColor;
	}
	.page-mode {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
	}
	.head-r {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
	}
	.cols {
		display: grid;
		grid-template-columns: 250px minmax(0, 1fr);
		gap: 22px;
		align-items: start;
	}
	.side {
		display: grid;
		gap: 12px;
	}
	@media (max-width: 860px) {
		.cols {
			grid-template-columns: minmax(0, 1fr);
		}
	}
	.panel {
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		padding: 12px 14px;
	}
	.panel h2 {
		margin: 0 0 8px;
		font-size: 13px;
		font-weight: 600;
	}
	.panel ul {
		margin: 0;
		padding: 0;
		list-style: none;
		display: grid;
		gap: 6px;
		font-size: 13px;
	}
	.panel li {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.panel small {
		margin-left: auto;
		color: var(--t3);
	}
	.wide :global(.seg) {
		display: grid;
		grid-template-columns: 1fr 1fr;
		width: 100%;
	}
	.wide :global(button),
	.compact :global(button) {
		text-align: center;
		white-space: nowrap;
		padding-left: 6px;
		padding-right: 6px;
	}
	.compact :global(.seg) {
		display: grid;
		grid-template-columns: 1fr 1fr;
	}
	.compact :global(button) {
		font-size: 12px;
		padding: 3px 6px;
	}
	.rail-hint {
		margin: 8px 0 0;
		color: var(--t2);
		font-size: 12.5px;
	}
	.e-mode {
		margin-bottom: 12px;
		padding-bottom: 12px;
		border-bottom: 1px solid var(--border-soft);
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
	.rseg {
		margin: 0 8px 6px;
	}
	.rl {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.ri {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 8px;
	}
	.mk {
		width: 14px;
		height: 14px;
		display: grid;
		place-items: center;
		color: var(--accent);
		flex: none;
	}
	.mk svg {
		width: 14px;
		height: 14px;
		fill: none;
		stroke: currentColor;
		stroke-width: 2;
		stroke-linecap: round;
	}
	.who {
		flex: 1;
		min-width: 0;
		display: grid;
		font-size: 13px;
	}
	.who b {
		font-weight: 600;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.who small {
		color: var(--t3);
		font-size: 12px;
	}
	.sw {
		position: relative;
		width: 34px;
		height: 20px;
		border-radius: 10px;
		border: 1px solid var(--border);
		background: var(--surface-3);
		flex: none;
		padding: 0;
	}
	.sw::after {
		content: '';
		position: absolute;
		top: 2px;
		left: 2px;
		width: 14px;
		height: 14px;
		border-radius: 50%;
		background: var(--t2);
	}
	.fw-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
	}
	.plus {
		margin: 4px 4px 0 0;
		width: 24px;
		height: 24px;
		border: 0;
		border-radius: 6px;
		background: none;
		color: var(--accent);
		font-size: 18px;
		line-height: 1;
	}
	.fw-hint {
		margin: 0 8px 6px;
		font-size: 12px;
		color: var(--t3);
	}
	.fw-empty {
		margin: 6px 8px;
		font-size: 12px;
		color: var(--dim);
	}
	.btn {
		font-size: 12px;
		font-weight: 500;
		background: var(--surface-2);
		border: 1px solid var(--border);
		border-radius: 6px;
		padding: 3px 9px;
		color: var(--text);
		white-space: nowrap;
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
	.search {
		display: flex;
		align-items: center;
		gap: 10px;
		height: 42px;
		padding: 0 14px;
		margin-bottom: 28px;
		border: 1px solid var(--border);
		border-radius: 10px;
		background: var(--surface);
		color: var(--t3);
	}
	.search.tight {
		margin-bottom: 12px;
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
	.list-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		flex-wrap: wrap;
		margin-bottom: 10px;
	}
	.list-head h2 {
		margin: 0;
		font-size: 13px;
		font-weight: 600;
	}
	.hint {
		margin: 0;
		color: var(--t2);
		font-size: 12.5px;
	}
	.b-head {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: 8px 12px;
		flex-wrap: wrap;
		margin-bottom: 12px;
	}
	.b-l {
		display: grid;
		gap: 6px;
		min-width: 0;
	}
	.mode-row {
		display: flex;
		align-items: center;
		gap: 12px;
		flex-wrap: wrap;
		color: var(--t2);
		font-size: 12.5px;
	}
	.mode-row.top {
		margin-bottom: 14px;
	}
	.mode-row.now-row {
		margin-bottom: 10px;
	}
	.mode-row.now-row .seg {
		margin-left: auto;
	}
	.mode-label {
		color: var(--text);
		font-size: 13px;
		font-weight: 600;
	}
	.page-mode .mode-label {
		color: var(--t2);
		font-size: 12.5px;
		font-weight: 500;
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
		padding: 4px 12px;
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
	}
	.game + .game {
		margin-top: 10px;
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
		padding: 2px 0;
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
	}
	.grip button:disabled {
		opacity: 0.3;
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
	.rm {
		margin-left: auto;
		color: var(--t3);
		font-size: 12px;
		background: none;
		border: 0;
		padding: 2px 4px;
	}
	.camps {
		margin: 0;
		padding: 0;
		list-style: none;
		display: grid;
		gap: 6px;
	}
	.camp {
		display: grid;
		grid-template-columns: 14px 1fr auto;
		gap: 10px;
		align-items: center;
		font-size: 13px;
	}
	.camp span:nth-child(2) {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.camp small {
		color: var(--t3);
		font: 12px var(--mono);
		white-space: nowrap;
	}
	.muted {
		color: var(--t2);
		font-size: 13px;
		margin: 0;
	}
	@media (max-width: 720px) {
		.frame {
			padding: 16px 12px;
		}
		.game {
			grid-template-columns: 36px 64px minmax(0, 1fr);
		}
		.game.row {
			grid-template-columns: 40px auto minmax(0, 1fr);
		}
		.artw,
		.artw :global(.art) {
			min-height: 0;
		}
		.body {
			padding: 12px 14px;
		}
		.camp {
			grid-template-columns: 14px 1fr;
		}
		.camp small {
			grid-column: 2;
			margin-top: -4px;
		}
	}
</style>
