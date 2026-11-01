<script lang="ts">
	import { page } from '$app/state';
	import Mock from '../_Mock.svelte';
	import Theme from '../_Theme.svelte';
	import { variants } from '../_data';

	const variant = $derived(variants.find((v) => v.id === page.params.v));
</script>

<main>
	{#if variant}
		<div class="bar">
			<p><a href="/sandbox/account-panel/">account-panel</a> / {variant.id}</p>
			<Theme />
		</div>
		<h1>{variant.name}</h1>
		<p class="trade">{variant.tradeoff}</p>
		<p class="note">{variant.note}</p>
		{#key variant.id}
			<Mock {variant} />
		{/key}
	{:else}
		<p>Unknown variant. <a href="/sandbox/account-panel/">Back</a></p>
	{/if}
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
	.trade {
		margin: 0 0 4px;
		font-weight: 500;
		color: var(--accent);
	}
	.note {
		color: var(--t2);
		margin: 0 0 16px;
		max-width: 80ch;
	}
	@media (max-width: 720px) {
		main {
			padding: 16px 16px 48px;
		}
	}
</style>
