<script lang="ts">
	import { setContext } from 'svelte';
	import GameCard from '$lib/components/GameCard.svelte';
	import PageHead from '$lib/components/PageHead.svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import type { Game, Runner } from '$lib/api/types';
	import gamesFx from '$lib/fixtures/games.json';
	import runnersFx from '$lib/fixtures/runners.mining.json';
	import { gameCards } from '$lib/home';
	import { LIVE_KEY, NET_KEY } from '$lib/live-context';
	import { styleOf, type Variant } from './_variants';

	let { variant }: { variant: Variant } = $props();

	// Static stand-ins for the live store: the sandbox never calls the API.
	setContext(LIVE_KEY, { overview: null, unreachable: false, retryIn: 0, updatedAt: null });
	setContext(NET_KEY, { down: false, retryIn: 0 });

	const cards = gameCards(gamesFx.games as unknown as Game[], runnersFx as unknown as Runner[]).slice(0, 3);
	const now = Date.now();
	const lastScan = new Date(now - 4 * 60_000).toISOString();

	const log = [
		{ at: '14:02:11', lvl: 'ok', who: 'hJune', msg: 'Claimed Rust Reward 1' },
		{ at: '14:01:40', lvl: 'info', who: 'grub_alt', msg: 'Watching seed_streamer, 42/60 min' },
		{ at: '13:58:03', lvl: 'warn', who: 'kick_main', msg: 'Stream went offline, switching channel' },
		{ at: '13:55:27', lvl: 'err', who: 'System', msg: 'Kick scan failed: timeout after 15s' }
	];
	const rows = [
		{ p: 'twitch' as const, n: 'hJune', d: 'Rust Reward 2', tag: 'Mining' },
		{ p: 'kick' as const, n: 'kick_main', d: 'Apex Legends Reward 1', tag: 'Waiting' },
		{ p: 'twitch' as const, n: 'grub_alt', d: 'Signed out', tag: 'Needs login' }
	];
</script>

<div class="scope" style={styleOf(variant)}>
	<PageHead title="Home" status="Mining" stamp="Updated just now" />

	<section>
		<h3 class="h">Games</h3>
		{#each cards as card (card.game.id)}
			<GameCard {card} {lastScan} nextScan={null} {now} />
		{/each}
	</section>

	<section>
		<h3 class="h">Accounts</h3>
		<div class="list">
			{#each rows as r (r.n)}
				<div class="row">
					<PlatformMark platform={r.p} />
					<b class={`pf-name ${r.p}`}>{r.n}</b>
					<span class="d">{r.d}</span>
					<span class="chip">{r.tag}</span>
				</div>
			{/each}
		</div>
	</section>

	<section>
		<h3 class="h">Buttons and status</h3>
		<div class="btns">
			<button type="button" class="b primary">Add account</button>
			<button type="button" class="b">Rescan</button>
			<button type="button" class="b danger">Remove</button>
			<span class="st green">Healthy</span>
			<span class="st amber">Degraded</span>
			<span class="st red">Offline</span>
			<span class="st blue">Info</span>
			<span class="st accent">Priority 1</span>
		</div>
	</section>

	<section>
		<h3 class="h">Log</h3>
		<ol class="log">
			{#each log as e (e.at)}
				<li class={e.lvl}>
					<time>{e.at}</time><span class="lv {e.lvl}"></span><span class="acc">{e.who}</span><span class="msg">{e.msg}</span>
				</li>
			{/each}
		</ol>
	</section>
</div>

<style>
	.scope {
		background: var(--bg);
		color: var(--text);
		color-scheme: light;
		padding: 18px 18px 24px;
		border-radius: 12px;
		font: 14px/1.5 var(--font);
	}
	section {
		margin-top: 20px;
	}
	.h {
		margin: 0 0 8px;
		font-size: 13px;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--t3);
	}
	.list {
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		overflow: hidden;
	}
	.row {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 10px 14px;
	}
	.row + .row {
		border-top: 1px solid var(--border-soft);
	}
	.d {
		flex: 1;
		color: var(--t2);
	}
	.chip {
		padding: 1px 8px;
		border-radius: 999px;
		background: var(--surface-2);
		color: var(--t2);
		font-size: 12px;
	}
	.btns {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		align-items: center;
	}
	.b {
		font: inherit;
		padding: 6px 12px;
		border-radius: 8px;
		border: 1px solid var(--border);
		background: var(--surface-2);
		color: var(--text);
		cursor: pointer;
	}
	.b.primary {
		background: var(--accent-strong);
		border-color: var(--accent-strong);
		color: #fff;
	}
	.b.danger {
		color: var(--red);
	}
	.st {
		padding: 2px 10px;
		border-radius: 999px;
		font-size: 12px;
		font-weight: 600;
	}
	.st.green {
		background: var(--green-soft);
		color: var(--green);
	}
	.st.amber {
		background: var(--amber-soft);
		color: var(--amber);
	}
	.st.red {
		background: var(--red-soft);
		color: var(--red);
	}
	.st.blue {
		background: var(--blue-soft);
		color: var(--blue);
	}
	.st.accent {
		background: var(--accent-soft);
		color: var(--accent);
	}
	.log {
		list-style: none;
		margin: 0;
		padding: 6px 0;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		font: 12.5px/1.5 var(--mono);
	}
	.log li {
		display: grid;
		grid-template-columns: 64px 8px 84px minmax(0, 1fr);
		gap: 10px;
		align-items: baseline;
		padding: 3px 14px;
	}
	.log time {
		color: var(--t3);
	}
	.lv {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		align-self: center;
		background: var(--t3);
	}
	.lv.ok {
		background: var(--green);
	}
	.lv.warn {
		background: var(--amber);
	}
	.lv.err {
		background: var(--red);
	}
	.acc {
		color: var(--t2);
	}
	li.err .msg {
		color: var(--red);
	}
</style>
