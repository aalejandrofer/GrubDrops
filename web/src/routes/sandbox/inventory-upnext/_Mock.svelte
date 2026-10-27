<script lang="ts">
	import BoxArt from '$lib/components/BoxArt.svelte';
	import Campaigns from './_Campaigns.svelte';
	import { accountPlatform, dropCount, games, hours, minutesLeft, type Variant } from './_data';

	let { variant }: { variant: Variant } = $props();

	/** variant a: expanded game rows */
	let expanded = $state<Record<string, boolean>>({});
	/** variant b: game shown in the drawer */
	let open = $state<string | null>(null);
	const current = $derived(games.find((g) => g.name === open) ?? null);
	const total = games.reduce((n, g) => n + dropCount(g), 0);

	function pick(name: string) {
		if (variant.id === 'a') expanded[name] = !expanded[name];
		else open = open === name ? null : name;
	}
	function onkey(e: KeyboardEvent) {
		if (e.key === 'Escape') open = null;
	}
</script>

<svelte:window onkeydown={onkey} />

<div class="frame">
	<h1 class="pg">Inventory</h1>

	<section class="panel" aria-label="Up next">
		<div class="panel-h">
			<h2>Up next</h2>
			<span class="n">{total}</span>
			<span class="sub">{games.length} games</span>
		</div>
		<ol class="rows">
			{#each games as g, i (g.name)}
				{@const on = variant.id === 'a' ? !!expanded[g.name] : open === g.name}
				<li class:on>
					<button type="button" class="row" aria-expanded={on} onclick={() => pick(g.name)}>
						<span class="qn">{i + 1}</span>
						<span class="art"><BoxArt src={g.art} name={g.name} size="sm" /></span>
						<span class="what">
							<span class="gn">{g.name} <span class="cnt">· {dropCount(g)} drops</span></span>
							<span class="meta">
								{g.campaigns.length} {g.campaigns.length === 1 ? 'campaign' : 'campaigns'} ·
								{#each g.accounts as a, j (a)}{j ? ', ' : ''}<span class="pf-name {accountPlatform[a]}">{a}</span>{/each}
							</span>
						</span>
						<span class="left">
							<b>{hours(minutesLeft(g))}</b>
							<small>to watch</small>
						</span>
						<span class="chev" class:up={on} aria-hidden="true">›</span>
					</button>
					{#if variant.id === 'a' && on}
						<div class="inline"><Campaigns {g} /></div>
					{/if}
				</li>
			{/each}
		</ol>
		<div class="panel-f">Permanent order lives on <a href="/games" class="link">Games</a>.</div>
	</section>

	<section class="claimed" aria-label="Claimed">
		<h2>Claimed <span class="n">128</span></h2>
		<p>Claimed rewards list, unchanged.</p>
	</section>
</div>

{#if variant.id === 'b' && current}
	<div class="scrim" role="presentation" onclick={(e) => e.target === e.currentTarget && (open = null)}>
		<div class="sheet" role="dialog" aria-modal="true" aria-label={current.name}>
			<header>
				<span class="dart"><BoxArt src={current.art} name={current.name} size="sm" /></span>
				<div>
					<h3>{current.name}</h3>
					<p>
						{dropCount(current)} drops · {current.campaigns.length}
						{current.campaigns.length === 1 ? 'campaign' : 'campaigns'} · {hours(minutesLeft(current))} to watch
					</p>
				</div>
				<button type="button" class="x" aria-label="Close" onclick={() => (open = null)}>×</button>
			</header>
			<Campaigns g={current} />
		</div>
	</div>
{/if}

<style>
	.frame {
		max-width: 760px;
		padding: 20px 16px 40px;
		border: 1px solid var(--border-soft);
		border-radius: 12px;
		background: var(--bg);
		color: var(--text);
	}
	.pg {
		margin: 0 0 16px;
		font-size: 18px;
		font-weight: 600;
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
		padding: 12px 14px 10px;
	}
	h2 {
		margin: 0;
		font-size: 13px;
		font-weight: 600;
	}
	.n {
		font: 500 11px/1 var(--mono);
		padding: 3px 6px;
		border-radius: 6px;
		background: var(--surface-3);
		color: var(--t2);
	}
	.sub {
		margin-left: auto;
		color: var(--t3);
		font-size: 12px;
	}
	.rows {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.rows > li {
		border-top: 1px solid var(--border-soft);
	}
	.rows > li.on {
		background: var(--surface-2);
	}
	.row {
		display: grid;
		grid-template-columns: 18px 36px minmax(0, 1fr) auto 14px;
		align-items: center;
		gap: 12px;
		width: 100%;
		padding: 10px 14px;
		background: none;
		border: 0;
		color: inherit;
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.row:hover .gn {
		text-decoration: underline;
	}
	.row:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.qn {
		font: 500 12px/1 var(--mono);
		color: var(--t3);
		text-align: center;
	}
	.art,
	.dart {
		display: block;
		width: 36px;
		height: 48px;
		border-radius: 4px;
		overflow: hidden;
	}
	.what {
		display: flex;
		flex-direction: column;
		gap: 3px;
		min-width: 0;
	}
	.gn {
		font-size: 13.5px;
		font-weight: 600;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.cnt {
		font-weight: 500;
		color: var(--t2);
	}
	.meta {
		font-size: 12px;
		color: var(--t3);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.left {
		display: flex;
		flex-direction: column;
		align-items: flex-end;
		gap: 2px;
	}
	.left b {
		font: 600 12.5px/1 var(--mono);
	}
	.left small {
		font-size: 11px;
		color: var(--t3);
	}
	.chev {
		color: var(--t3);
		font-size: 16px;
		transition: transform 0.15s;
	}
	.chev.up {
		transform: rotate(90deg);
	}
	.inline {
		padding: 2px 14px 12px 80px;
	}
	.panel-f {
		padding: 10px 14px;
		border-top: 1px solid var(--border-soft);
		font-size: 12px;
		color: var(--t3);
	}
	.link {
		color: var(--t2);
	}
	.claimed {
		margin-top: 24px;
	}
	.claimed h2 {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.claimed p {
		margin: 8px 0 0;
		padding: 18px 14px;
		border: 1px dashed var(--border-soft);
		border-radius: 10px;
		color: var(--t3);
		font-size: 12.5px;
	}
	.scrim {
		position: fixed;
		inset: 0;
		z-index: 20;
		display: grid;
		place-items: stretch end;
		background: rgba(0, 0, 0, 0.3);
	}
	.sheet {
		width: min(520px, 100%);
		height: 100%;
		overflow: auto;
		padding: 20px;
		border-left: 1px solid var(--border);
		background: var(--surface-2);
		color: var(--text);
	}
	header {
		display: grid;
		grid-template-columns: 36px minmax(0, 1fr) auto;
		gap: 12px;
		align-items: start;
		margin-bottom: 16px;
	}
	h3 {
		margin: 0;
		font-size: 15px;
	}
	header p {
		margin: 3px 0 0;
		font-size: 12px;
		color: var(--t2);
	}
	.x {
		width: 28px;
		height: 28px;
		border: 1px solid var(--border);
		border-radius: 6px;
		background: var(--surface);
		color: var(--t2);
		font-size: 16px;
		cursor: pointer;
	}
	@media (max-width: 560px) {
		.frame {
			padding: 16px 12px 32px;
		}
		.row {
			grid-template-columns: 36px minmax(0, 1fr) auto 12px;
			gap: 10px;
			padding: 10px 12px;
		}
		.qn {
			display: none;
		}
		.inline {
			padding: 2px 12px 12px;
		}
	}
</style>
