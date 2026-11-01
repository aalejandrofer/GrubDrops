<script lang="ts">
	import type { Variant } from './_data';
	import { PanelState } from './_state.svelte';
	import A from './_A.svelte';
	import B from './_B.svelte';
	import C from './_C.svelte';

	let { variant }: { variant: Variant } = $props();
	const st = new PanelState();
</script>

<div class="frame">
	{#if variant.id === 'a'}<A {st} />{:else if variant.id === 'b'}<B {st} />{:else}<C {st} />{/if}
</div>

<style>
	.frame {
		padding: 24px;
		border: 1px solid var(--border);
		border-radius: 12px;
		background: var(--bg);
		color: var(--text);
	}
	@media (max-width: 720px) {
		.frame {
			padding: 14px;
		}
	}
</style>
