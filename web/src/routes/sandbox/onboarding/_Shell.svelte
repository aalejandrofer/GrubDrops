<script lang="ts">
	import type { Snippet } from 'svelte';
	import logo from '../../../../../assets/img/logo.png';
	import { t } from '$lib/i18n/index.svelte';
	import { fake } from './_variants';
	import './_ob.css';

	let { tabs = true, children }: { tabs?: boolean; children: Snippet } = $props();
	const items = ['nav.home', 'nav.games', 'nav.inventory', 'nav.settings'];
</script>

<div class="ob">
	<header class="top">
		<div class="top-in">
			<span class="brand"><img src={logo} alt="" /><span>{t('app.name')}</span></span>
			{#if tabs}
				<nav class="tabs" aria-label="Main">
					{#each items as k, i (k)}<a href="#tab" class:on={i === 0} aria-current={i === 0 ? 'page' : undefined}>{t(k)}</a>{/each}
				</nav>
				<span class="ver">{fake.version}</span>
			{/if}
		</div>
	</header>
	<div class="body">{@render children()}</div>
</div>

<style>
	.top {
		border-bottom: 1px solid var(--border-soft);
	}
	.top-in {
		max-width: 1160px;
		margin: 0 auto;
		height: 52px;
		padding: 0 24px;
		display: flex;
		align-items: center;
		gap: 28px;
	}
	.brand {
		display: flex;
		align-items: center;
		gap: 8px;
		font-weight: 600;
		letter-spacing: -0.01em;
	}
	.brand img {
		width: 24px;
		height: 24px;
		border-radius: 6px;
	}
	.tabs {
		display: flex;
		gap: 2px;
	}
	.tabs a {
		padding: 6px 10px;
		border-radius: 6px;
		color: var(--t2);
		font-weight: 500;
		font-size: 13.5px;
	}
	.tabs a.on {
		color: var(--text);
		background: var(--surface-2);
	}
	.ver {
		margin-left: auto;
		font: 12px var(--mono);
		color: var(--t3);
	}
	.body {
		max-width: 1160px;
		margin: 0 auto;
		padding: 28px 24px 40px;
	}
	@container (max-width: 640px) {
		.top-in {
			padding: 0 16px;
			gap: 14px;
		}
		.brand span,
		.ver {
			display: none;
		}
		.body {
			padding: 20px 16px 32px;
		}
	}
</style>
