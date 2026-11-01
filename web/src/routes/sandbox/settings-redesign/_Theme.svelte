<script lang="ts">
	import { onMount } from 'svelte';
	import { st } from './_state.svelte';

	/** Flips the app tokens. `?theme=light` in the URL picks light on load. */
	onMount(() => {
		const q = new URLSearchParams(location.search).get('theme');
		if (q === 'light' || q === 'dark') st.setTheme(q);
		else st.theme = document.documentElement.dataset.theme === 'light' ? 'light' : 'dark';
	});
</script>

<div class="seg" role="group" aria-label="Theme">
	<button type="button" aria-pressed={st.theme === 'dark'} onclick={() => st.setTheme('dark')}>Dark</button>
	<button type="button" aria-pressed={st.theme === 'light'} onclick={() => st.setTheme('light')}>Light</button>
</div>

<style>
	.seg {
		display: inline-flex;
		border: 1px solid var(--border);
		border-radius: 8px;
		overflow: hidden;
		flex: none;
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
