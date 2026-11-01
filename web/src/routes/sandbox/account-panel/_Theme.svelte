<script lang="ts">
	import { onMount } from 'svelte';

	/** Flips the app tokens between dark and light, like Settings does. */
	let theme = $state<'dark' | 'light'>('dark');
	onMount(() => {
		theme = document.documentElement.dataset.theme === 'light' ? 'light' : 'dark';
	});
	function set(t: 'dark' | 'light'): void {
		theme = t;
		document.documentElement.dataset.theme = t;
	}
</script>

<div class="seg" role="group" aria-label="Theme">
	<button type="button" aria-pressed={theme === 'dark'} onclick={() => set('dark')}>Dark</button>
	<button type="button" aria-pressed={theme === 'light'} onclick={() => set('light')}>Light</button>
</div>

<style>
	.seg {
		display: inline-flex;
		border: 1px solid var(--border);
		border-radius: 8px;
		overflow: hidden;
	}
	button {
		font: inherit;
		font-size: 12.5px;
		padding: 0 12px;
		height: 28px;
		border: 0;
		background: var(--surface);
		color: var(--t2);
		cursor: pointer;
	}
	button + button {
		border-left: 1px solid var(--border);
	}
	button[aria-pressed='true'] {
		background: var(--accent-soft);
		color: var(--accent);
	}
</style>
