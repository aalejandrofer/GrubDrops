<script lang="ts">
	import Screen from './_Screen.svelte';
	import { states, type Variant } from './_variants';

	let { variant, min = variant.min }: { variant: Variant; min?: number } = $props();
</script>

<div class="frames" style={`--min:${min}px`}>
	{#each states as s (s.id)}
		<figure>
			<figcaption>{s.name}</figcaption>
			<div class="frame"><Screen variant={variant.id} state={s.id} /></div>
		</figure>
	{/each}
</div>

<style>
	.frames {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(min(var(--min), 100%), 1fr));
		gap: 16px;
	}
	figure {
		margin: 0;
	}
	figcaption {
		font-size: 12px;
		color: #b5b5bb;
		margin-bottom: 6px;
	}
	.frame {
		height: 540px;
		border: 1px solid #3a3a40;
		border-radius: 10px;
		overflow: hidden;
	}
</style>
