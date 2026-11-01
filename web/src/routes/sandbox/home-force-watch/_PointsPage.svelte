<script lang="ts">
	import PointsMock from './_PointsMock.svelte';
	import { pvariants } from './_points';

	let { id }: { id?: 'a' | 'b' | 'c' } = $props();
	const shown = $derived(id ? pvariants.filter((v) => v.id === id) : pvariants);
</script>

<main>
	<p>
		<a href="/sandbox/">Sandbox</a> / <a href="/sandbox/home-force-watch/">home-force-watch</a> /
		{#if id}<a href="/sandbox/home-force-watch/points">points</a> / {id}{:else}points{/if}
	</p>
	<h1>{id ? shown[0].name : 'Channel points on the Points watch tile, round 2'}</h1>
	<p class="sub">
		Round 2, 2026-11-01. Home KPI row with the tile redesigned to show channel points. Switch the scene above each mockup.
		Desktop row first, then a 390px phone frame. Variants: {#each pvariants as v, i (v.id)}{i ? ', ' : ''}<a
				href={`/sandbox/home-force-watch/points-${v.id}`}>{v.id.toUpperCase()}</a
			>{/each}.
	</p>
	{#each shown as v (v.id)}
		<section>
			{#if !id}<h2>{v.name}</h2>{/if}
			<p class="note">{v.note}</p>
			<p class="note"><b>Tradeoff:</b> {v.tradeoff}</p>
			<p class="note"><b>Data:</b> {v.backend}</p>
			{#key v.id}<PointsMock variant={v} />{/key}
		</section>
	{/each}
</main>

<style>
	main {
		padding: 24px 20px 48px;
		background: #1c1c1f;
		color: #e6e6e6;
		min-height: 100vh;
	}
	a {
		text-decoration: underline;
	}
	h1 {
		margin: 4px 0;
		font-size: 22px;
	}
	.sub,
	.note {
		color: #b5b5bb;
		margin: 0 0 8px;
		max-width: 80ch;
	}
	.note b {
		color: #e6e6e6;
		font-weight: 600;
	}
	section {
		margin-top: 28px;
	}
	h2 {
		margin: 0 0 4px;
		font-size: 16px;
	}
</style>
