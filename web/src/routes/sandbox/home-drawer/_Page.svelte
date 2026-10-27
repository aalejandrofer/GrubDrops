<script lang="ts">
	import Mock from './_Mock.svelte';
	import { variants } from './_data';

	let { id }: { id: 'a' | 'b' } = $props();
	const variant = $derived(variants.find((v) => v.id === id)!);
</script>

<main>
	<p><a href="/sandbox/">Sandbox</a> / home-drawer / {variant.id}</p>
	<h1>Home: open the campaign drawer</h1>
	<p class="sub">
		GRUB-27. Static slice of Home with sample data; the drawer is the real one. Variants:
		<a href="/sandbox/home-drawer/" aria-current={id === 'a' ? 'page' : undefined}>A</a>,
		<a href="/sandbox/home-drawer/b" aria-current={id === 'b' ? 'page' : undefined}>B</a>.
	</p>
	<h2>{variant.name}</h2>
	<p class="note">{variant.note}</p>
	{#key variant.id}
		<Mock {variant} />
	{/key}
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
	a[aria-current] {
		font-weight: 600;
	}
	h1 {
		margin: 4px 0;
		font-size: 22px;
	}
	h2 {
		margin: 20px 0 2px;
		font-size: 16px;
	}
	.sub,
	.note {
		color: #b5b5bb;
		margin: 0 0 12px;
		max-width: 75ch;
	}
</style>
