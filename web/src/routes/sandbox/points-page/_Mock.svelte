<script lang="ts">
	import type { Variant } from './_data';
	import { PointsState } from './_state.svelte';
	import Shell from './_Shell.svelte';
	import A from './_A.svelte';
	import B from './_B.svelte';
	import C from './_C.svelte';
	import D from './_D.svelte';

	let { variant }: { variant: Variant } = $props();
	const st = new PointsState();
</script>

<div class="frame">
	<Shell {st}>
		{#if variant.id === 'a'}<A {st} />{:else if variant.id === 'b'}<B {st} />{:else if variant.id === 'c'}<C {st} />{:else}<D {st} />{/if}
	</Shell>
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
