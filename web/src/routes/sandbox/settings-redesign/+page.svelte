<script lang="ts">
	import './_ui.css';
	import Theme from './_Theme.svelte';
	import { variants } from './_variants';
</script>

<main>
	<div class="bar">
		<p><a href="/sandbox/">Sandbox</a> / settings-redesign</p>
		<Theme />
	</div>
	<h1>Settings, more colour and four layouts</h1>
	<p class="sub">
		Settings grew to eight sections and about 40 controls on one scroll. One colour pass of today’s layout, then three
		layouts built for a bigger surface. Same fake data in all four: TTik3r and Phluses on Twitch and Kick (one Kick
		login expired), Discord connected, Telegram not, Kick canary failing. Switches, segments, search and navigation all
		respond; nothing is saved. Each page works at desktop and 390px phone width.
	</p>
	<ul class="cards">
		{#each variants as v, i (v.id)}
			<li>
				<a class="card" href={`/sandbox/settings-redesign/${v.id}`}>
					<span class="id">{i === 0 ? '0' : v.name.slice(0, 1)}</span>
					<span class="txt">
						<b>{i === 0 ? v.name : v.name.slice(4)}</b>
						<span class="line">{v.line}</span>
						<span class="trade">{v.tradeoff}</span>
					</span>
				</a>
			</li>
		{/each}
	</ul>
</main>

<style>
	main {
		padding: 24px 20px 48px;
		min-height: 100vh;
		max-width: 900px;
		margin: 0 auto;
	}
	.bar a {
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
		margin: 0 0 20px;
		max-width: 80ch;
	}
	.cards {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 10px;
	}
	.card {
		display: flex;
		gap: 14px;
		align-items: flex-start;
		padding: 14px 16px;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
	}
	.card:hover {
		border-color: var(--accent);
	}
	.id {
		flex: none;
		width: 28px;
		height: 28px;
		border-radius: 8px;
		display: grid;
		place-items: center;
		font-weight: 700;
		color: var(--accent);
		background: var(--accent-soft);
	}
	.txt {
		display: grid;
		gap: 2px;
	}
	.line {
		color: var(--t2);
	}
	.trade {
		color: var(--t3);
		font-size: 13px;
	}
</style>
