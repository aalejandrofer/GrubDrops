<script lang="ts">
	import Mock from './_Mock.svelte';
	import { variants } from './_data';

	let { id }: { id: 'a' | 'b' } = $props();
	const v = $derived(variants.find((x) => x.id === id)!);
</script>

<main>
	<p><a href="/sandbox/">Sandbox</a> / inventory-upnext / {v.id}</p>
	<h1>Inventory Up next grouped by game</h1>
	<p class="sub">
		Static data. Home's Up next stays per drop. Variants:
		{#each variants as x, i (x.id)}{i ? ', ' : ''}<a href={x.id === 'a' ? '/sandbox/inventory-upnext/' : `/sandbox/inventory-upnext/${x.id}`} class:cur={x.id === v.id}
				>{x.name}</a
			>{/each}.
	</p>
	<h2>{v.name}</h2>
	<p class="note">{v.note}</p>
	{#key v.id}
		<Mock variant={v} />
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
	a.cur {
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
	@media (max-width: 560px) {
		main {
			padding: 16px 12px 32px;
		}
	}
</style>
