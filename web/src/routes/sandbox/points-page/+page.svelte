<script lang="ts">
	import Mock from './_Mock.svelte';
	import Theme from './_Theme.svelte';
	import { variants } from './_data';
</script>

<main>
	<div class="bar">
		<p><a href="/sandbox/">Sandbox</a> / points-page</p>
		<Theme />
	</div>
	<h1>Points page, four layouts</h1>
	<p class="sub">
		GRUB-43. A top-level Points page with Twitch and Kick tabs, home of the Force watch list. Static data: 3 Twitch
		accounts (Drops + points, Points only, Off and paused), 2 Kick accounts. Toggle accounts, switch modes, drag or
		arrow to reorder, add and remove channels. Full page per variant: {#each variants as v, i (v.id)}{i ? ', ' : ''}<a
				href={`/sandbox/points-page/${v.id}`}>{v.id}</a
			>{/each}.
	</p>
	{#each variants as v (v.id)}
		<section>
			<h2>{v.name}</h2>
			<p class="trade">{v.tradeoff}</p>
			<p class="note">{v.note}</p>
			<Mock variant={v} />
		</section>
	{/each}
</main>

<style>
	main {
		padding: 24px 20px 48px;
		min-height: 100vh;
		max-width: 1200px;
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
	.sub,
	.note {
		color: var(--t2);
		margin: 0 0 12px;
		max-width: 80ch;
	}
	.trade {
		margin: 0 0 4px;
		font-weight: 500;
		color: var(--accent);
	}
	section {
		margin-top: 40px;
	}
	h2 {
		margin: 0 0 2px;
		font-size: 16px;
	}
	@media (max-width: 720px) {
		main {
			padding: 16px 16px 48px;
		}
	}
</style>
