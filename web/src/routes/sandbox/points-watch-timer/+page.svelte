<script lang="ts">
	import Theme from '../points-page/_Theme.svelte';
	import { variants } from './_data';
</script>

<main>
	<div class="bar">
		<p><a href="/sandbox/">Sandbox</a> / points-watch-timer</p>
		<Theme />
	</div>
	<h1>Points watch timer, four placements</h1>
	<p class="sub">
		How long each account has been on its current Points watch channel, as "37m" or "1h 12m". Fake data, live
		ticking once a minute on the minute. Each page has clock buttons to jump ahead and see the longer formats.
	</p>
	<p class="sub">
		Backend: no new field. <code>runner.started_at</code> is already set when the runner enters force_watching
		(<code>beginForce</code> in <code>internal/runner/force.go</code>), resets when it moves to another channel, and
		the UI formats it with <code>uptime()</code> from <code>$lib/format</code>. A miner restart resets it too.
	</p>
	<ul>
		{#each variants as v (v.id)}
			<li>
				<a href={`/sandbox/points-watch-timer/${v.id}`}><b>{v.name}</b></a>
				<p class="trade">{v.tradeoff}</p>
				<p class="note">{v.note}</p>
			</li>
		{/each}
	</ul>
</main>

<style>
	main {
		padding: 24px 20px 48px;
		min-height: 100vh;
		max-width: 960px;
		margin: 0 auto;
	}
	a {
		text-decoration: underline;
	}
	.bar {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}
	.bar p {
		margin: 0;
	}
	h1 {
		margin: 8px 0 4px;
		font-size: 22px;
	}
	.sub {
		color: var(--t2);
		margin: 0 0 12px;
		max-width: 80ch;
	}
	code {
		font-family: var(--mono);
		font-size: 0.92em;
	}
	ul {
		list-style: none;
		margin: 24px 0 0;
		padding: 0;
		display: grid;
		gap: 10px;
	}
	li {
		padding: 14px 16px;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
	}
	.trade {
		margin: 6px 0 4px;
		font-weight: 500;
		color: var(--accent);
	}
	.note {
		margin: 0;
		color: var(--t2);
	}
	@media (max-width: 720px) {
		main {
			padding: 16px 16px 48px;
		}
	}
</style>
