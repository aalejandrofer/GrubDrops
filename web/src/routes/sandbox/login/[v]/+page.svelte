<script lang="ts">
	import { page } from '$app/state';
	import Frames from '../_Frames.svelte';
	import { variants } from '../_variants';

	const variant = $derived(variants.find((v) => v.id === page.params.v));
</script>

<main>
	{#if variant}
		<p><a href="/sandbox/login/">login</a> / {variant.id}</p>
		<h1>{variant.name}</h1>
		<p class="note">{variant.note}</p>
		{#key variant.id}
			<Frames {variant} min={Math.max(variant.min, 560)} />
		{/key}
	{:else}
		<p>Unknown variant. <a href="/sandbox/login/">Back</a></p>
	{/if}
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
	.note {
		color: #b5b5bb;
		margin: 0 0 16px;
	}
</style>
