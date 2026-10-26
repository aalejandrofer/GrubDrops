<script lang="ts">
	import A from './_A.svelte';
	import B from './_B.svelte';
	import C from './_C.svelte';
	import { steps, type Variant } from './_variants';

	let { variant, min = 560 }: { variant: Variant; min?: number } = $props();
	const View = $derived(variant.id === 'a' ? A : variant.id === 'b' ? B : C);
</script>

<div class="frames" style={`--min:${min}px`}>
	{#each steps as s (s.id)}
		<figure>
			<figcaption>{s.name}</figcaption>
			<div class="frame"><View step={s.id} /></div>
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
		min-height: 640px;
		display: grid;
		border: 1px solid #3a3a40;
		border-radius: 10px;
		overflow: hidden;
		background: var(--bg);
	}
</style>
