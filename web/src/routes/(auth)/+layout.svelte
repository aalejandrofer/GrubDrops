<script lang="ts">
	import logo from '../../../../assets/img/logo.png';
	import { t } from '$lib/i18n/index.svelte';

	let { children } = $props();
</script>

<div class="wrap">
	<div class="card">
		<div class="brand"><img src={logo} alt="" /><span>{t('app.name')}</span></div>
		{@render children()}
	</div>
</div>

<style>
	.wrap {
		min-height: 100vh;
		display: grid;
		place-items: start center;
		padding: 15vh 16px 32px;
	}
	.card {
		width: 100%;
		max-width: 360px;
		background: var(--surface);
		border: 1px solid var(--border-soft);
		border-radius: 10px;
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
</style>
