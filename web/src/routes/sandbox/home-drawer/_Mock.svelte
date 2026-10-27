<script lang="ts">
	import { onDestroy, setContext } from 'svelte';
	import { api } from '$lib/app';
	import BoxArt from '$lib/components/BoxArt.svelte';
	import CampaignDrawer, { type CampaignTarget } from '$lib/components/CampaignDrawer.svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { LIVE_KEY } from '$lib/live-context';
	import { campaigns, queue, type Variant } from './_data';

	let { variant }: { variant: Variant } = $props();

	// Static stand-ins: the drawer reads runners from the live store and the campaign from the API.
	setContext(LIVE_KEY, { runners: null, overview: null, unreachable: false, retryIn: 0, updatedAt: null });
	const real = api.campaign;
	api.campaign = async (_p, id) => {
		await new Promise((r) => setTimeout(r, 200));
		return structuredClone(campaigns[id]);
	};
	onDestroy(() => (api.campaign = real));

	let target = $state<CampaignTarget | null>(null);
	const open = (id: string, highlight: string | null = null) => (target = { platform: 'twitch', id, highlight });
	const isA = $derived(variant.id === 'a');
</script>

{#snippet camp(id: string, text: string, highlight: string | null = null)}
	{#if isA}
		<button type="button" class="clink" data-campaign onclick={() => open(id, highlight)}>{text}</button>
	{:else}
		<span class="ctext">{text}</span>
	{/if}
{/snippet}

{#snippet info(id: string, label: string, highlight: string | null = null)}
	<button type="button" class="info" data-campaign aria-label={`Campaign details: ${label}`} title="Campaign details" onclick={() => open(id, highlight)}>
		<svg viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" aria-hidden="true"
			><rect x="1.5" y="1.5" width="9" height="9" rx="2" /><path d="M7.5 1.5v9" /></svg
		>
	</button>
{/snippet}

<div class="frame">
	<div class="layout">
		<div class="cards">
			<h2 class="sec">Your games <span class="n">2</span></h2>
			<article class="game p1">
				<div class="artw"><BoxArt src={null} name="SMITE 2" size="lg" /></div>
				<div class="body">
					<div class="status">
						<h3 class="gname">SMITE 2</h3>
						<span class="state mining">Mining</span>
						<span class="camp">{@render camp('hachiman', 'Nov Wk 1 Hachiman!')}</span>
						{#if !isA}{@render info('hachiman', 'Nov Wk 1 Hachiman!')}{/if}
					</div>
					<div class="accts">
						{#each [{ n: 'hJune', m: 42 }, { n: 'grub_alt', m: 18 }] as a (a.n)}
							<div class="acct">
								<div class="who"><PlatformMark platform="twitch" /><b>{a.n}</b></div>
								<div class="drop">
									<div class="l"><span>Hachiman Loading Frame</span><span>{a.m}/60</span></div>
									<div class="bar"><i style:width={`${(a.m / 60) * 100}%`}></i></div>
								</div>
								<div class="eta"><span>{60 - a.m} min</span><small>left</small></div>
							</div>
						{/each}
					</div>
				</div>
			</article>
			<article class="game p2">
				<div class="artw"><BoxArt src="https://static-cdn.jtvnw.net/ttv-boxart/516575_IGDB-144x192.jpg" name="VALORANT" size="lg" /></div>
				<div class="body">
					<div class="status">
						<h3 class="gname">VALORANT</h3>
						<span class="state waiting">Waiting</span>
						<span class="camp">{@render camp('vct', 'VCT26 GCC Sao Paulo')}</span>
						{#if !isA}{@render info('vct', 'VCT26 GCC Sao Paulo')}{/if}
					</div>
					<p class="idle-l">Starts when an account is free.</p>
				</div>
			</article>
		</div>

		<aside class="panel">
			<div class="panel-h"><h2>Up next</h2><span class="n">{queue.length}</span></div>
			<ol class="queue">
				{#each queue as q, i (q.drop)}
					<li>
						<span class="grip" title="Drag to reorder" role="presentation">
							<svg viewBox="0 0 12 12" fill="currentColor" aria-hidden="true"
								><circle cx="4" cy="3" r="1" /><circle cx="8" cy="3" r="1" /><circle cx="4" cy="6" r="1" /><circle cx="8" cy="6" r="1" /><circle
									cx="4"
									cy="9"
									r="1"
								/><circle cx="8" cy="9" r="1" /></svg
							>
						</span>
						<span class="qn">{i + 1}</span>
						<div class="qt">
							<div class="d">{q.name}</div>
							<div class="g">{q.game} · {@render camp(q.camp, campaigns[q.camp].name, q.drop)}</div>
							<div class="m">hJune · {q.min} min</div>
						</div>
						<div class="mv">
							{#if !isA}{@render info(q.camp, campaigns[q.camp].name, q.drop)}{/if}
							<button type="button" aria-label="Move up" title="Move up" disabled={i === 0}>
								<svg viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 7.5 6 4.5l3 3" /></svg>
							</button>
							<button type="button" aria-label="Mark collected" title="Mark collected">
								<svg viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m2.8 6.2 2.2 2.2 4.2-4.6" /></svg>
							</button>
						</div>
					</li>
				{/each}
			</ol>
			<div class="panel-f">Permanent order lives on <a href="/games">Games</a>.</div>
		</aside>
	</div>
</div>

<CampaignDrawer {target} onclose={() => (target = null)} />

<style>
	.frame {
		padding: 24px;
		border: 1px solid var(--border);
		border-radius: 12px;
		background: var(--bg);
		color: var(--text);
	}
	.layout {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 320px;
		gap: 24px;
		align-items: start;
	}
	.sec {
		margin: 0 0 12px;
		font-size: 13px;
		font-weight: 600;
	}
	.n {
		font: 12px var(--mono);
		color: var(--t3);
	}
	.clink {
		font: inherit;
		color: inherit;
		background: none;
		border: 0;
		padding: 0;
		cursor: pointer;
		text-decoration: underline dotted;
		text-decoration-color: var(--t3);
		text-underline-offset: 2px;
		max-width: 100%;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.clink:hover {
		color: var(--text);
		text-decoration-style: solid;
		text-decoration-color: currentColor;
	}
	.clink:focus-visible,
	.info:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
		border-radius: 3px;
	}
	.info {
		flex: none;
		width: 22px;
		height: 22px;
		display: grid;
		place-items: center;
		background: none;
		border: 1px solid transparent;
		border-radius: 4px;
		color: var(--t2);
		cursor: pointer;
	}
	.info:hover {
		border-color: var(--border);
		color: var(--text);
		background: var(--surface-2);
	}
	.info svg {
		width: 12px;
		height: 12px;
	}
	.game {
		display: grid;
		grid-template-columns: 168px minmax(0, 1fr);
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		overflow: hidden;
	}
	.game + .game {
		margin-top: 12px;
	}
	.artw {
		position: relative;
		overflow: hidden;
		display: grid;
	}
	.p1 .artw {
		min-height: 200px;
	}
	.p2 .artw {
		min-height: 150px;
	}
	.body {
		padding: 18px 20px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		justify-content: center;
	}
	.p1 .body {
		justify-content: flex-start;
	}
	.status {
		display: flex;
		align-items: center;
		gap: 6px 8px;
		flex-wrap: wrap;
		min-width: 0;
	}
	.gname {
		margin: 0 4px 0 0;
		font-size: 18px;
		font-weight: 600;
		white-space: nowrap;
	}
	.state {
		font-size: 12px;
		font-weight: 500;
		padding: 2px 8px;
		border-radius: 10px;
		display: inline-flex;
		align-items: center;
		gap: 6px;
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
	.camp {
		color: var(--t2);
		font-size: 13px;
		min-width: 0;
		display: inline-flex;
	}
	.ctext {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.accts {
		margin-top: 14px;
		display: grid;
		gap: 12px;
	}
	.acct {
		display: grid;
		grid-template-columns: 136px minmax(0, 1fr) 56px;
		gap: 14px;
		align-items: center;
	}
	.who {
		display: flex;
		align-items: center;
		gap: 7px;
		min-width: 0;
	}
	.who b {
		font-weight: 500;
	}
	.drop {
		min-width: 0;
	}
	.drop .l {
		display: flex;
		justify-content: space-between;
		gap: 8px;
		font-size: 13px;
	}
	.drop .l > span:first-child {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.drop .l > span:last-child {
		color: var(--t2);
		font: 12px var(--mono);
	}
	.bar {
		height: 5px;
		border-radius: 3px;
		background: var(--surface-3);
		margin-top: 6px;
		overflow: hidden;
	}
	.bar i {
		display: block;
		height: 100%;
		background: var(--accent);
	}
	.eta {
		display: grid;
		text-align: right;
		font: 12px var(--mono);
	}
	.eta small {
		color: var(--t3);
		font: 11px var(--font);
	}
	.idle-l {
		margin: 8px 0 0;
		color: var(--t2);
		font-size: 13px;
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
	.panel-h h2 {
		margin: 0;
		font-size: 13px;
		font-weight: 600;
	}
	.queue {
		list-style: none;
		margin: 0;
		padding: 0;
		border-top: 1px solid var(--border-soft);
	}
	.queue li {
		display: grid;
		grid-template-columns: 14px 14px minmax(0, 1fr) auto;
		gap: 8px;
		align-items: center;
		padding: 10px 8px 10px 10px;
	}
	.queue li + li {
		border-top: 1px solid var(--border-soft);
	}
	.grip {
		color: var(--t3);
		cursor: grab;
		display: grid;
		place-items: center;
	}
	.grip svg {
		width: 12px;
		height: 12px;
	}
	.qn {
		font: 12px var(--mono);
		color: var(--t3);
		text-align: right;
	}
	.qt {
		min-width: 0;
	}
	.d,
	.g,
	.m {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.d {
		font-weight: 500;
		font-size: 13px;
	}
	.g {
		font-size: 12px;
		color: var(--t2);
	}
	.m {
		font-size: 12px;
		color: var(--t3);
		margin-top: 1px;
	}
	.mv {
		display: flex;
		justify-content: flex-end;
		gap: 2px;
		min-width: 46px;
	}
	.mv button:not(.info) {
		width: 22px;
		height: 22px;
		display: grid;
		place-items: center;
		background: none;
		border: 1px solid transparent;
		border-radius: 4px;
		color: var(--t2);
		cursor: pointer;
	}
	.mv button:disabled {
		visibility: hidden;
	}
	.mv svg {
		width: 12px;
		height: 12px;
	}
	.panel-f {
		padding: 10px 14px;
		font-size: 12px;
		color: var(--t3);
		border-top: 1px solid var(--border-soft);
	}
	.panel-f a {
		color: var(--t2);
	}
	@media (max-width: 760px) {
		.frame {
			padding: 14px;
		}
		.layout {
			grid-template-columns: minmax(0, 1fr);
		}
		.game {
			grid-template-columns: 96px minmax(0, 1fr);
		}
		.p1 .artw,
		.p2 .artw {
			min-height: 120px;
		}
		.body {
			padding: 14px;
		}
		.gname {
			font-size: 16px;
		}
		.acct {
			grid-template-columns: minmax(0, 1fr) auto;
			gap: 4px 10px;
		}
		.acct .drop {
			grid-column: 1 / -1;
			grid-row: 2;
		}
	}
</style>
