<script lang="ts">
	import { page } from '$app/state';
	import '../_ui.css';
	import Colorful from '../_Colorful.svelte';
	import Hub from '../_Hub.svelte';
	import Scroll from '../_Scroll.svelte';
	import Sidebar from '../_Sidebar.svelte';
	import Theme from '../_Theme.svelte';
	import { variants } from '../_variants';

	const variant = $derived(variants.find((v) => v.id === page.params.v));
</script>

<main>
	{#if variant}
		<div class="bar">
			<p><a href="/sandbox/">Sandbox</a> / <a href="/sandbox/settings-redesign/">settings-redesign</a> / {variant.id}</p>
			<Theme />
		</div>
		<h1>{variant.name}</h1>
		<p class="note">{variant.note}</p>
		<p class="trade">{variant.tradeoff}</p>
		<div class="frame">
			{#key variant.id}
				{#if variant.id === 'colorful'}<Colorful />
				{:else if variant.id === 'sidebar'}<Sidebar />
				{:else if variant.id === 'hub'}<Hub />
				{:else}<Scroll />{/if}
			{/key}
		</div>
	{:else}
		<p>Unknown variant. <a href="/sandbox/settings-redesign/">Back</a></p>
	{/if}
</main>

<style>
	main {
		padding: 24px 20px 48px;
		min-height: 100vh;
		max-width: 1180px;
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
		font-size: 13px;
		color: var(--t2);
	}
	h1 {
		margin: 8px 0 4px;
		font-size: 22px;
	}
	.note {
		color: var(--t2);
		margin: 0 0 6px;
		max-width: 86ch;
	}
	.trade {
		margin: 0 0 22px;
		font-weight: 500;
		color: var(--accent);
		max-width: 86ch;
	}
	.frame {
		border-top: 1px dashed var(--border);
		padding-top: 24px;
	}
	@media (max-width: 720px) {
		main {
			padding: 16px 16px 48px;
		}
		.frame {
			padding-top: 16px;
		}
	}
</style>
