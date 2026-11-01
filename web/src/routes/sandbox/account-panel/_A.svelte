<script lang="ts">
	import { fly, fade } from 'svelte/transition';
	import type { PanelState } from './_state.svelte';
	import Shell from './_Shell.svelte';
	import Head from './_Head.svelte';
	import Login from './_Login.svelte';
	import Mining from './_Mining.svelte';
	import Points from './_Points.svelte';
	import Danger from './_Danger.svelte';

	/** A: the row opens a drawer on the right; the list stays usable behind it on desktop. */
	let { st }: { st: PanelState } = $props();
	const a = $derived(st.get(st.open));
</script>

<Shell {st}>
	{#if a}
		<button type="button" class="scrim" aria-label="Close" transition:fade={{ duration: 120 }} onclick={() => st.close()}></button>
		<aside class="drawer" aria-label="{a.name} details" transition:fly={{ x: 40, duration: 160 }}>
			<div class="top">
				<span class="crumb">Accounts / {a.platform === 'twitch' ? 'Twitch' : 'Kick'}</span>
				<button type="button" class="ico" aria-label="Close details" onclick={() => st.close()}>
					<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12" /></svg>
				</button>
			</div>
			{#key a.id}
				<div class="body">
					<Head {st} {a} />
					<Login {st} {a} />
					<Mining {st} {a} />
					<Points {a} />
					<Danger {st} {a} />
				</div>
			{/key}
			<p class="foot">Changes save as you make them.</p>
		</aside>
	{/if}
</Shell>

<style>
	.drawer {
		position: fixed;
		top: 0;
		right: 0;
		bottom: 0;
		z-index: 50;
		width: min(440px, 100vw);
		display: flex;
		flex-direction: column;
		background: var(--bg);
		border-left: 1px solid var(--border);
		box-shadow: -16px 0 40px rgb(0 0 0 / 0.35);
	}
	.top {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		padding: 10px 12px 10px 20px;
		border-bottom: 1px solid var(--border-soft);
	}
	.crumb {
		font-size: 12px;
		color: var(--t3);
	}
	.body {
		flex: 1;
		overflow-y: auto;
		overscroll-behavior: contain;
		padding: 20px;
		display: grid;
		align-content: start;
		gap: 24px;
	}
	.body > :global(.sec + .sec) {
		padding-top: 20px;
		border-top: 1px solid var(--border-soft);
	}
	.foot {
		margin: 0;
		padding: 10px 20px;
		border-top: 1px solid var(--border-soft);
		font-size: 12px;
		color: var(--t3);
	}
	.scrim {
		display: none;
	}
	@media (max-width: 720px) {
		.scrim {
			display: block;
			position: fixed;
			inset: 0;
			z-index: 49;
			border: 0;
			background: rgb(0 0 0 / 0.5);
		}
		.body {
			padding: 16px;
		}
		.top {
			padding-left: 16px;
		}
	}
</style>
