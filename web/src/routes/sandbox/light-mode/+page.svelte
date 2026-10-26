<script lang="ts">
	import Preview from './_Preview.svelte';
	import { variants } from './_variants';
</script>

<main>
	<p><a href="/sandbox/">Sandbox</a> / light-mode</p>
	<h1>Light mode, three darker options</h1>
	<p class="sub">
		Baseline plus three variants. Each is the real Home components under a scoped token override. Dark theme is untouched.
		Full page per variant: {#each variants.slice(1) as v, i (v.id)}{i ? ', ' : ''}<a href={`/sandbox/light-mode/${v.id}`}>{v.id}</a>{/each}.
	</p>
	<div class="cols">
		{#each variants as v (v.id)}
			<section>
				<h2>{v.name}</h2>
				<p class="note">{v.note}</p>
				<Preview variant={v} />
			</section>
		{/each}
	</div>
</main>

<style>
	main {
		padding: 24px 20px;
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
		margin: 0 0 12px;
	}
	.cols {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(420px, 1fr));
		gap: 20px;
		align-items: start;
	}
	h2 {
		margin: 0 0 2px;
		font-size: 16px;
	}
</style>
