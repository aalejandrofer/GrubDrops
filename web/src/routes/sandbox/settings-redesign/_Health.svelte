<script lang="ts">
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import Ico from './_Ico.svelte';
	import { CANARY_EVERY, LOG, LOG_KIND } from './_data';
	import { st } from './_state.svelte';

	let filter = $state<'all' | 'problems' | 'claims'>('all');
	const rows = $derived(
		LOG.filter((r) =>
			filter === 'all' ? true : filter === 'claims' ? r.kind === 'claim' : r.kind === 'error' || r.kind === 'auth'
		)
	);
	let running = $state(false);
	function run(): void {
		running = true;
		setTimeout(() => (running = false), 1400);
	}
</script>

<div class="stats">
	<div class="stat ok"><span>Miner</span><b>Running 3d 4h</b></div>
	<div class="stat"><span>Kick sidecar</span><b>1 running</b></div>
	<div class="stat"><span>Last scan</span><b>2 min ago</b></div>
</div>

<div class="panel" id="set-canary">
	<div class="row">
		<div class="lbl">
			<b>Canary</b>
			<small>Watches a near-always-live channel to confirm watch time still counts.</small>
		</div>
		<button type="button" class="btn tint" disabled={running} onclick={run}>
			<Ico name={running ? 'refresh' : 'play'} size={13} />
			{running ? 'Running…' : 'Run now'}
		</button>
	</div>
	<div class="row can">
		<PlatformMark platform="twitch" />
		<div class="lbl"><b>rocketleague</b></div>
		<span class="chip ok">OK 2h ago</span>
	</div>
	<div class="row can">
		<PlatformMark platform="kick" />
		<div class="lbl">
			<b>xqc</b>
			<small class="bad">No watch time credit after 10 min.</small>
		</div>
		<span class="chip bad">Failing</span>
	</div>
</div>

<div class="panel" id="set-log">
	<div class="row filters">
		<div class="lbl"><b>Activity</b></div>
		<div class="seg" role="group" aria-label="Filter log">
			<button type="button" aria-pressed={filter === 'all'} onclick={() => (filter = 'all')}>All</button>
			<button type="button" aria-pressed={filter === 'problems'} onclick={() => (filter = 'problems')}>Problems</button>
			<button type="button" aria-pressed={filter === 'claims'} onclick={() => (filter = 'claims')}>Claims</button>
		</div>
	</div>
	{#each rows as r, i (i)}
		<div class="row ev">
			<time>{r.at}</time>
			<span class="chip plain {LOG_KIND[r.kind].tone}">{LOG_KIND[r.kind].label}</span>
			<span class="msg">
				{#if r.who}<span class="pf-name {r.who.platform}">{r.who.name}</span>{:else}<span class="sys">system</span>{/if}
				{r.msg}
			</span>
		</div>
	{/each}
</div>

<details class="adv">
	<summary>Advanced</summary>
	<div class="panel" id="set-canary-cfg">
		<div class="row">
			<div class="lbl">
				<b>Run the canary every</b>
				<small>Applies after a restart.</small>
			</div>
			<select class="sel" aria-label="Run the canary every" bind:value={st.canaryEvery}>
				{#each CANARY_EVERY as c}<option value={c}>{c}</option>{/each}
			</select>
		</div>
		<div class="card-body">
			<div class="two">
				<div class="field">
					<label for="sx-ct">Twitch canary channel</label>
					<input id="sx-ct" class="inp" value={st.canaryTwitch} />
				</div>
				<div class="field">
					<label for="sx-ck">Kick canary channel</label>
					<input
						id="sx-ck"
						class="inp"
						class:dirty={st.barMode && 'canaryKick' in st.dirty}
						value={st.canaryKick}
						oninput={(e) => st.set('canaryKick', e.currentTarget.value)}
					/>
				</div>
			</div>
			<p class="help">Pick channels that are almost always live.</p>
		</div>
	</div>
</details>

<style>
	.stats {
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		gap: 10px;
		margin-bottom: 12px;
	}
	.stat {
		padding: 10px 14px;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		display: grid;
		gap: 2px;
		min-width: 0;
	}
	.stat span {
		font-size: 12px;
		color: var(--t2);
	}
	.stat b {
		font-weight: 600;
		font-size: 14px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.stat.ok b {
		color: var(--green);
	}
	.can {
		padding-top: 9px !important;
		padding-bottom: 9px !important;
	}
	.can b {
		font-family: var(--mono);
		font-size: 12.5px !important;
	}
	small.bad {
		color: var(--red) !important;
	}
	.ev {
		padding-top: 8px !important;
		padding-bottom: 8px !important;
		gap: 10px !important;
		flex-wrap: nowrap !important;
		align-items: baseline !important;
	}
	.ev time {
		font: 12px var(--mono);
		color: var(--t3);
		flex: none;
	}
	.ev .chip {
		flex: none;
		min-width: 74px;
		justify-content: center;
	}
	.msg {
		font-size: 13px;
		min-width: 0;
	}
	.sys {
		color: var(--t3);
	}
	@media (max-width: 720px) {
		.stats {
			grid-template-columns: 1fr 1fr;
		}
		.stat:first-child {
			grid-column: 1 / -1;
		}
		.ev {
			flex-wrap: wrap !important;
		}
		.msg {
			flex-basis: 100%;
		}
	}
</style>
