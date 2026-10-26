<script lang="ts">
	import { page } from '$app/state';
	import Preview from '../_Preview.svelte';
	import { variants } from '../_variants';

	const variant = $derived(variants.find((v) => v.id === page.params.v));
</script>

{#if variant}
	{#key variant.id}
		<Preview {variant} />
	{/key}
{:else}
	<p>Unknown variant. <a href="/sandbox/light-mode/">Back</a></p>
{/if}

<style>
	:global(body) {
		background: #1c1c1f;
	}
</style>
