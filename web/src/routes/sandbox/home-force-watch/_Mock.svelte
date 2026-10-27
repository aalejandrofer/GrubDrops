<script lang="ts">
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { all, liveCount, mining, rows, scenes, type Scene, type Variant } from './_variants';

	let { variant }: { variant: Variant } = $props();

	let scene = $state<Scene>('watching');
	const list = $derived(rows(scene));
	const has = $derived(all(scene).length > 0);
	const more = $derived(liveCount(scene) - list.length);
	const watching = $derived(list.filter((r) => r.watchers.length > 0));
	const nWatch = $derived(watching.reduce((n, r) => n + r.watchers.length, 0));
</script>

{#snippet who(r: (typeof list)[number])}
	{#if r.watchers.length}
		<span class="on">{r.watchers.join(', ')}</span>
	{:else}
		<span class="off">Nobody watching</span>
	{/if}
{/snippet}

<div class="scenes" role="group" aria-label="Scene">
	{#each scenes as s (s.id)}
		<button type="button" class:sel={scene === s.id} aria-pressed={scene === s.id} onclick={() => (scene = s.id)}>{s.label}</button>
	{/each}
</div>

<div class="frame">
	<div class="head">
		<h1>Home</h1>
		<span class="now"><span class="dot"></span>Mining · {mining(scene)} of 4 accounts</span>
		{#if variant.id === 'c' && list.length}
			<a class="now fw" href="/games">
				<span class="dot live"></span><span
					>{#if watching.length}Force watching {watching.map((r) => r.name).join(', ')} · {nWatch} account{nWatch > 1 ? 's' : ''}{:else}{list.length} forced channel{list.length > 1 ? 's' : ''} live · nobody watching{/if}</span
				>
			</a>
		{/if}
	</div>

	<div class="kpis" class:four={variant.id === 'a' && has}>
		<div class="kpi">
			<div class="label">Minutes watched</div>
			<div class="val">12,480<small>min</small></div>
			<div class="sub">310 today</div>
		</div>
		<div class="kpi">
			<div class="label">Total drops claimed</div>
			<div class="val">142</div>
			<div class="sub">3 today</div>
		</div>
		{#if variant.id === 'a' && has}
			<a class="kpi" href="/games">
				<div class="label">Force watch</div>
				{#if list.length}
					<ul class="tile">
						{#each list as r (r.platform + r.name)}
							<li>
								<span class="dot live"></span><PlatformMark platform={r.platform} size="sm" /><b>{r.name}</b>
								{@render who(r)}
							</li>
						{/each}
					</ul>
					{#if more > 0}<div class="sub more">{more} more live</div>{/if}
				{:else}
					<div class="val sm">None live</div>
					<div class="sub">{all(scene).length} channels waiting</div>
				{/if}
			</a>
		{/if}
		<div class="kpi">
			<div class="label">Accounts healthy</div>
			<div class="val">4<small>/ 4</small></div>
			<div class="sub">All signed in</div>
		</div>
	</div>

	<div class="layout">
		<div class="cards" aria-hidden="true">
			<div class="ph"></div>
			<div class="ph"></div>
			<div class="ph"></div>
		</div>
		<aside>
			{#if variant.id === 'b' && has}
				<section class="panel">
					<div class="panel-h"><h2>Force watch</h2><a href="/games" class="lnk">Edit</a></div>
					{#if list.length}
						<ul class="fw">
							{#each list as r (r.platform + r.name)}
								<li>
									<span class="dot live"></span><PlatformMark platform={r.platform} /><span class="nm"
										><b>{r.name}</b>{@render who(r)}</span
									>
								</li>
							{/each}
						</ul>
					{:else}
						<p class="none">No forced channel live.</p>
					{/if}
				</section>
			{/if}
			<section class="panel">
				<div class="panel-h"><h2>Up next</h2><span class="n">5</span></div>
				<div class="ph sm"></div>
				<div class="ph sm"></div>
			</section>
			<section class="panel">
				<div class="panel-h"><h2>Claimed</h2></div>
				<div class="ph sm"></div>
			</section>
		</aside>
	</div>
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
	.frame {
		padding: 24px;
		border: 1px solid var(--border);
		border-radius: 12px;
		background: var(--bg);
		color: var(--text);
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
	.now.fw {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		line-height: 1;
		color: var(--t2);
		text-decoration: none;
	}
	.now.fw:hover {
		color: var(--text);
	}
	.dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: currentColor;
		flex: none;
	}
	.dot.live {
		background: var(--red);
		width: 7px;
		height: 7px;
	}
	.kpis {
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		border: 1px solid var(--border-soft);
		border-radius: var(--radius);
		background: var(--surface);
		margin-bottom: 28px;
	}
	.kpis.four {
		grid-template-columns: 1fr 1fr 1.3fr 1fr;
	}
	.kpi {
		padding: 16px 20px 18px;
		display: block;
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
	}
	a.kpi:hover .label {
		color: var(--text);
	}
	.kpi .val {
		font-size: 30px;
		font-weight: 600;
		letter-spacing: -0.03em;
		line-height: 1.15;
		margin-top: 6px;
	}
	.kpi .val.sm {
		font-size: 20px;
		color: var(--t2);
	}
	.kpi .val small {
		font-size: 14px;
		font-weight: 500;
		color: var(--t2);
		margin-left: 2px;
	}
	.kpi .sub {
		font-size: 12px;
		color: var(--t3);
		margin-top: 2px;
	}
	.tile {
		list-style: none;
		margin: 8px 0 0;
		padding: 0;
		display: grid;
		gap: 6px;
	}
	.tile li {
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: 13px;
		min-width: 0;
	}
	.tile li > :last-child {
		margin-left: auto;
		font-size: 12px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.more {
		margin-top: 6px;
	}
	.on {
		color: var(--green);
		font-weight: 500;
	}
	.off {
		color: var(--t3);
	}
	.layout {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 320px;
		gap: 24px;
		align-items: start;
	}
	.cards,
	aside {
		display: grid;
		gap: 12px;
	}
	.ph {
		height: 120px;
		border-radius: 10px;
		background: var(--surface);
		border: 1px solid var(--border-soft);
	}
	.ph.sm {
		height: 44px;
		margin-top: 8px;
	}
	.panel {
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		padding: 12px 14px;
	}
	.panel-h {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.panel-h h2 {
		margin: 0;
		font-size: 13px;
		font-weight: 600;
	}
	.panel-h .n {
		color: var(--t3);
		font-size: 12px;
	}
	.lnk {
		margin-left: auto;
		font-size: 12px;
		color: var(--accent);
	}
	.fw {
		list-style: none;
		margin: 8px 0 0;
		padding: 0;
		display: grid;
		gap: 2px;
	}
	.fw li {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 6px 0;
	}
	.fw .nm {
		display: grid;
		font-size: 12px;
	}
	.fw .nm b {
		font-size: 13px;
	}
	.none {
		margin: 8px 0 0;
		font-size: 12px;
		color: var(--t3);
	}
	@media (max-width: 760px) {
		.frame {
			padding: 14px;
		}
		.kpis,
		.kpis.four {
			grid-template-columns: 1fr 1fr;
		}
		.kpi:nth-child(3) {
			border-left: 0;
		}
		.kpis.four .kpi:nth-child(3),
		.kpis.four .kpi:nth-child(4) {
			border-top: 1px solid var(--border-soft);
		}
		.kpis:not(.four) .kpi:nth-child(3) {
			grid-column: 1 / -1;
			border-top: 1px solid var(--border-soft);
		}
		.kpis.four .kpi:nth-child(4) {
			border-left: 1px solid var(--border-soft);
		}
		.layout {
			grid-template-columns: minmax(0, 1fr);
		}
	}
</style>
