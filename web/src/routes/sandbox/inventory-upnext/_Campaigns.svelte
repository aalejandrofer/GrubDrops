<script lang="ts">
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { accountPlatform, type Game } from './_data';

	let { g }: { g: Game } = $props();
</script>

{#each g.campaigns as c (c.id)}
	<div class="camp">
		<div class="ch">
			<PlatformMark platform={c.platform} size="sm" />
			<span class="cn">{c.name}</span>
			<span class="ce">{c.drops.length} drops · ends {c.ends}</span>
		</div>
		<ol>
			{#each c.drops as d, i (i)}
				{@const pct = Math.round((d.watched / d.minutes) * 100)}
				<li>
					<span class="dn">{d.name}</span>
					<span class="da pf-name {accountPlatform[d.account]}">{d.account}</span>
					<span class="dm">
						<span class="bar"><i style="width:{pct}%"></i></span>
						{d.watched ? `${d.watched} / ${d.minutes} min` : `${d.minutes} min`}
					</span>
				</li>
			{/each}
		</ol>
	</div>
{/each}

<style>
	.camp + .camp {
		margin-top: 12px;
	}
	.ch {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
		font-size: 12.5px;
	}
	.cn {
		font-weight: 600;
	}
	.ce {
		color: var(--t3);
		font-size: 12px;
	}
	ol {
		list-style: none;
		margin: 6px 0 0;
		padding: 0;
	}
	li {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto 128px;
		align-items: center;
		gap: 10px;
		padding: 6px 0;
		font-size: 12.5px;
		border-top: 1px solid var(--border-soft);
	}
	.dn {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.da {
		font-size: 12px;
	}
	.dm {
		display: grid;
		grid-template-columns: 36px 1fr;
		align-items: center;
		gap: 8px;
		text-align: right;
		color: var(--t2);
		font: 500 11.5px/1 var(--mono);
		white-space: nowrap;
	}
	.bar {
		width: 36px;
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
	@media (max-width: 560px) {
		li {
			grid-template-columns: minmax(0, 1fr) auto;
			row-gap: 4px;
		}
		.dm {
			grid-column: 1 / -1;
			grid-template-columns: 64px auto;
			justify-content: start;
			text-align: left;
		}
		.bar {
			width: 64px;
		}
	}
</style>
