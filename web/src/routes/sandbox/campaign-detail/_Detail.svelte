<script lang="ts">
	import BoxArt from '$lib/components/BoxArt.svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { accounts, type Campaign } from './_data';

	let { c, highlight = null, flat = false }: { c: Campaign; highlight?: string | null; flat?: boolean } = $props();

	const acct = (id: string) => accounts.find((a) => a.id === id)!;
	const claimed = $derived(c.drops.reduce((n, d) => n + Object.values(d.cells).filter((x) => x.state === 'claimed').length, 0));
	const blocked = $derived(c.drops.some((d) => Object.values(d.cells).some((x) => x.state === 'link')));
</script>

<div class="cd" class:flat>
	{#if !flat}
		<header>
			<PlatformMark platform={c.platform} />
			<div>
				<h3>{c.name}</h3>
				<p>{c.game} · {c.starts} to {c.ends}{#if c.ended} · <span class="ended">ended</span>{/if}</p>
			</div>
		</header>
	{/if}
	<p class="sum">{c.drops.length} drops · {claimed} claimed across accounts</p>
	{#if blocked && c.linkUrl}
		<p class="link">
			<span>Needs a linked game account before Kick counts watch time.</span>
			<a href={c.linkUrl} target="_blank" rel="noopener noreferrer">Link account</a>
		</p>
	{/if}
	<ol>
		{#each c.drops as d (d.id)}
			<li class:hl={highlight === d.id}>
				<span class="img"><BoxArt src={null} name={d.name} size="sm" /></span>
				<div class="what">
					<span class="nm">{d.name}{#if highlight === d.id}<span class="you">this reward</span>{/if}</span>
					<span class="req">{d.minutes} min</span>
				</div>
				<div class="cells">
					{#each Object.entries(d.cells) as [id, x] (id)}
						{@const a = acct(id)}
						<span class="cell {x.state}">
							<span class="who"><PlatformMark platform={a.platform} size="sm" />{a.name}</span>
							{#if x.state === 'claimed'}
								<span class="st">Claimed {x.at}</span>
							{:else if x.state === 'progress'}
								<span class="bar"><i style="width:{Math.round((x.min / d.minutes) * 100)}%"></i></span>
								<span class="st">{x.min}/{d.minutes}</span>
							{:else if x.state === 'link'}
								<span class="st">Needs link</span>
							{:else}
								<span class="st">Not started</span>
							{/if}
						</span>
					{:else}
						<span class="cell idle"><span class="st">{c.ended ? 'Not earned' : 'No account on this game'}</span></span>
					{/each}
				</div>
			</li>
		{/each}
	</ol>
</div>

<style>
	.cd {
		display: grid;
		gap: 10px;
	}
	header {
		display: flex;
		gap: 10px;
		align-items: flex-start;
	}
	header :global(.pm) {
		margin-top: 4px;
	}
	h3 {
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
	.cells {
		display: grid;
		gap: 4px;
	}
	.cell {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto;
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
	.st {
		color: var(--muted);
		font-family: var(--mono);
		text-align: right;
	}
	.claimed .st {
		color: var(--green);
	}
	.link .st,
	.cell.link .st {
		color: var(--amber);
	}
	.progress .st {
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
		li {
			grid-template-columns: 46px minmax(0, 1fr);
		}
		.cells {
			grid-column: 1 / -1;
		}
	}
</style>
