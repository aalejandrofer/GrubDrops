<script lang="ts">
	import BoxArt from '$lib/components/BoxArt.svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import Detail from './_Detail.svelte';
	import { accounts, campaigns, rewards, type Campaign, type Variant } from './_data';

	let { variant }: { variant: Variant } = $props();

	/** open overlay: campaign id plus the reward to highlight */
	let open = $state<{ c: string; hl: string | null } | null>(null);
	/** inline (variant c): keys of expanded campaign lines and reward rows */
	let expanded = $state<Record<string, boolean>>({});

	const camp = (id: string) => campaigns.find((c) => c.id === id)!;
	const drop = (c: Campaign, id: string) => c.drops.find((d) => d.id === id)!;
	const acct = (id: string) => accounts.find((a) => a.id === id)!;
	const games = ['Rust', 'Marvel Rivals'];
	const live = (g: string) => campaigns.filter((c) => c.game === g && !c.ended);
	const current = $derived(open ? camp(open.c) : null);

	function show(c: string, hl: string | null, key: string) {
		if (variant.id === 'c') expanded[key] = !expanded[key];
		else open = { c, hl };
	}
	function onkey(e: KeyboardEvent) {
		if (e.key === 'Escape') open = null;
	}
</script>

<svelte:window onkeydown={onkey} />

<div class="frame">
	<section>
		<h2 class="pg">Games</h2>
		<div class="games">
			{#each games as g, i (g)}
				<article class="game">
					<div class="artw"><BoxArt src={null} name={g} size="lg" /><span class="rank">0{i + 1}</span></div>
					<div class="body">
						<h3>{g}</h3>
						{#if live(g).length}
							<ul class="camps">
								{#each live(g) as c (c.id)}
									{@const k = `g:${c.id}`}
									<li>
										<button type="button" class="camp" aria-expanded={variant.id === 'c' ? !!expanded[k] : undefined} onclick={() => show(c.id, null, k)}>
											<PlatformMark platform={c.platform} /><span class="cn">{c.name}</span>
											<small>{c.drops.length} drops · ends {c.ends}</small>
											<span class="chev" class:up={expanded[k]} aria-hidden="true">›</span>
										</button>
										{#if variant.id === 'c' && expanded[k]}
											<div class="inline"><Detail {c} flat /></div>
										{/if}
									</li>
								{/each}
							</ul>
						{:else}
							<p class="muted">Nothing live right now</p>
						{/if}
					</div>
				</article>
			{/each}
		</div>
	</section>

	<section>
		<h2 class="pg">Rewards · Claimed</h2>
		<div class="panel">
			{#each rewards as r (`${r.account}:${r.drop}`)}
				{@const c = camp(r.campaign)}
				{@const d = drop(c, r.drop)}
				{@const a = acct(r.account)}
				{@const k = `r:${r.account}:${r.drop}`}
				<button type="button" class="hrow" aria-expanded={variant.id === 'c' ? !!expanded[k] : undefined} onclick={() => show(c.id, d.id, k)}>
					<span class="img"><BoxArt src={null} name={d.name} size="sm" /></span>
					<span class="rwt"><span class="nm">{d.name}</span><small>{c.name}</small></span>
					<span class="gm">{c.game}</span>
					<span class="ac"><PlatformMark platform={a.platform} size="sm" />{a.name}</span>
					<span class="tm">{r.at}</span>
				</button>
				{#if variant.id === 'c' && expanded[k]}
					<div class="inline row"><Detail {c} highlight={d.id} /></div>
				{/if}
			{/each}
		</div>
	</section>

	{#if current && open}
		<div class="scrim" class:drawer={variant.id === 'b'} role="presentation" onclick={(e) => e.target === e.currentTarget && (open = null)}>
			<div class="sheet" role="dialog" aria-modal="true" aria-label={current.name}>
				<button type="button" class="x" aria-label="Close" onclick={() => (open = null)}>×</button>
				<Detail c={current} highlight={open.hl} />
			</div>
		</div>
	{/if}
</div>

<style>
	.frame {
		position: relative;
		display: grid;
		gap: 28px;
		padding: 20px;
		border: 1px solid var(--border);
		border-radius: 12px;
		background: var(--bg);
		color: var(--text);
		overflow: hidden;
	}
	.pg {
		margin: 0 0 10px;
		font-size: 18px;
	}
	.games {
		display: grid;
		gap: 10px;
	}
	.game {
		display: grid;
		grid-template-columns: 110px minmax(0, 1fr);
		border: 1px solid var(--border);
		border-radius: var(--radius);
		background: var(--surface);
		overflow: hidden;
	}
	.artw {
		position: relative;
	}
	.rank {
		position: absolute;
		left: 6px;
		top: 6px;
		font-family: var(--mono);
		font-size: 12px;
		background: rgba(0, 0, 0, 0.6);
		color: #fff;
		padding: 0 5px;
		border-radius: 4px;
	}
	.body {
		padding: 12px;
		min-width: 0;
	}
	h3 {
		margin: 0 0 8px;
		font-size: 15px;
	}
	.camps {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 4px;
	}
	.camp {
		width: 100%;
		display: flex;
		gap: 8px;
		align-items: center;
		flex-wrap: wrap;
		padding: 6px 8px;
		border: 1px solid transparent;
		border-radius: 6px;
		background: none;
		color: inherit;
		font: inherit;
		font-size: 13px;
		text-align: left;
		cursor: pointer;
	}
	.camp:hover,
	.hrow:hover {
		background: var(--surface-2);
		border-color: var(--border-soft);
	}
	.cn {
		font-weight: 500;
	}
	small {
		color: var(--muted);
		font-size: 12px;
	}
	.chev {
		margin-left: auto;
		color: var(--muted);
		transition: transform 0.15s;
	}
	.chev.up {
		transform: rotate(90deg);
	}
	.muted {
		color: var(--muted);
		font-size: 13px;
		margin: 0;
	}
	.inline {
		padding: 8px 8px 12px;
	}
	.inline.row {
		padding: 10px 12px 14px;
		background: var(--surface-2);
		border-bottom: 1px solid var(--border-soft);
	}
	.panel {
		border: 1px solid var(--border);
		border-radius: var(--radius);
		background: var(--surface);
		overflow: hidden;
	}
	.hrow {
		width: 100%;
		display: grid;
		grid-template-columns: 40px minmax(0, 2fr) minmax(0, 1fr) minmax(0, 1fr) auto;
		gap: 12px;
		align-items: center;
		padding: 8px 12px;
		border: 0;
		border-bottom: 1px solid var(--border-soft);
		background: none;
		color: inherit;
		font: inherit;
		font-size: 13px;
		text-align: left;
		cursor: pointer;
	}
	.img {
		width: 40px;
		height: 40px;
		border-radius: 6px;
		overflow: hidden;
	}
	.rwt {
		display: grid;
		min-width: 0;
	}
	.nm {
		font-weight: 600;
	}
	.gm,
	.tm {
		color: var(--t2);
	}
	.tm {
		font-family: var(--mono);
		font-size: 12px;
	}
	.ac {
		display: inline-flex;
		gap: 5px;
		align-items: center;
	}
	.scrim {
		position: absolute;
		inset: 0;
		background: rgba(0, 0, 0, 0.55);
		display: grid;
		place-items: center;
		padding: 24px;
		z-index: 5;
	}
	.sheet {
		position: relative;
		width: min(640px, 100%);
		max-height: 100%;
		overflow: auto;
		padding: 20px;
		border: 1px solid var(--border);
		border-radius: 12px;
		background: var(--surface-2);
	}
	.scrim.drawer {
		place-items: stretch end;
		padding: 0;
		background: rgba(0, 0, 0, 0.3);
	}
	.drawer .sheet {
		width: min(520px, 100%);
		max-height: none;
		border-radius: 0;
		border-width: 0 0 0 1px;
	}
	.x {
		position: absolute;
		right: 10px;
		top: 8px;
		border: 0;
		background: none;
		color: var(--muted);
		font-size: 22px;
		cursor: pointer;
	}
	@media (max-width: 640px) {
		.frame {
			padding: 12px;
		}
		.game {
			grid-template-columns: 72px minmax(0, 1fr);
		}
		.hrow {
			grid-template-columns: 40px minmax(0, 1fr) auto;
		}
		.gm,
		.ac {
			display: none;
		}
		.scrim {
			padding: 0;
		}
		.sheet {
			width: 100%;
			height: 100%;
			max-height: none;
			border-radius: 0;
		}
	}
</style>
