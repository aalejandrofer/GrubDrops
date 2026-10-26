<script lang="ts">
	import logo from '../../../../assets/img/logo.png';
	import { t } from '$lib/i18n/index.svelte';
	import './auth.css';

	let { children } = $props();
	const host = typeof location === 'undefined' ? '' : location.host;
</script>

<div class="wrap auth">
	<div class="card">
		<div class="brand"><img src={logo} alt="" /><span>{t('app.name')}</span></div>
		{@render children()}
	</div>
	{#if host}<p class="foot">{host}</p>{/if}
</div>

<style>
	.wrap {
		min-height: 100vh;
		display: flex;
		flex-direction: column;
		align-items: center;
		padding: 15vh 16px 32px;
	}
	.card {
		width: 100%;
		max-width: 360px;
		background: var(--surface);
		border: 1px solid var(--border-soft);
		border-radius: 12px;
		padding: 24px;
	}
	.brand {
		display: flex;
		align-items: center;
		gap: 8px;
		font-weight: 600;
		letter-spacing: -0.01em;
		margin-bottom: 20px;
	}
	.brand img {
		width: 24px;
		height: 24px;
		border-radius: 6px;
	}
	.foot {
		margin: 14px 0 0;
		font: 12px var(--mono);
		color: var(--t3);
		overflow-wrap: anywhere;
		text-align: center;
	}
</style>
