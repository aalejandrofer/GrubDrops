<script lang="ts">
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { uptime } from '$lib/format';
	import Pill from './_Pill.svelte';
	import { pts, type Acc } from './_data';

	/** Home KPI row with the Points watch tile, as live today, plus an optional timer per row. */
	let { accs, now, start, timer }: { accs: Acc[]; now: number; start: (x: Acc) => string; timer: boolean } = $props();

	const live = $derived(accs.filter((x) => x.channel !== null));
	const bal: Record<string, number | null> = { shroud: 41_870, trainwreckstv: null };
</script>

<div class="kpis">
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
	<a class="kpi" href="/points">
		<div class="label">
			<span>Points watch{#if timer}<Pill />{/if}</span>
			<span class="tot">48,210 pts</span>
		</div>
		<ul class="tile">
			{#each live as x (x.a.id)}
				{@const b = bal[x.channel ?? '']}
				<li>
					<span class="dot"></span><PlatformMark platform={x.a.platform} size="sm" /><b>{x.channel}</b>
					<span class="who"
						><span class="on">{x.a.name}</span>{#if timer}<span class="tm">{uptime(start(x), now)}</span>{/if}</span
					>
					{#if b != null}<span class="bal">{pts(b)}</span>{/if}
				</li>
			{/each}
		</ul>
	</a>
	<a class="kpi" href="/settings#accounts">
		<div class="label">Accounts mining</div>
		<div class="val">2<small>/ 2</small></div>
		<div class="sub">2 healthy</div>
	</a>
</div>

<style>
	/* Copied from routes/(app)/+page.svelte so the tile reads as it does live. */
	.kpis {
		display: grid;
		grid-template-columns: repeat(4, minmax(0, 1fr));
		border: 1px solid var(--border-soft);
		border-radius: var(--radius);
		background: var(--surface);
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
		display: flex;
		align-items: center;
		gap: 6px;
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
	.kpi .sub {
		font-size: 12px;
		color: var(--t3);
		margin-top: 2px;
	}
	a.kpi:hover .label {
		color: var(--text);
	}
	a.kpi:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.tot {
		margin-left: auto;
		color: var(--text);
		font-weight: 500;
		font-variant-numeric: tabular-nums;
		white-space: nowrap;
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
	.tile b {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		min-width: 0;
	}
	.tile .dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--red);
		flex: none;
	}
	.tile .who {
		margin-left: auto;
		font-size: 12px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		min-width: 0;
	}
	.tile .on {
		color: var(--green);
		font-weight: 500;
	}
	/* The new part: time on this channel, quieter than the name. */
	.tile .tm {
		margin-left: 5px;
		color: var(--t2);
		font-variant-numeric: tabular-nums;
	}
	.tile .bal {
		flex: none;
		margin-left: 6px;
		font-size: 12px;
		color: var(--t2);
		font-variant-numeric: tabular-nums;
	}
	@media (max-width: 860px) {
		.kpis {
			grid-template-columns: 1fr 1fr;
		}
		.kpi:nth-child(3) {
			border-left: 0;
		}
		.kpi:nth-child(3),
		.kpi:nth-child(4) {
			border-top: 1px solid var(--border-soft);
		}
	}
	@media (max-width: 520px) {
		.kpis {
			grid-template-columns: 1fr;
		}
		.kpi + .kpi {
			border-left: 0;
			border-top: 1px solid var(--border-soft);
		}
	}
</style>
