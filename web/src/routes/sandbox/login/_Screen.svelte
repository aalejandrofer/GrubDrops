<script lang="ts">
	import logo from '../../../../../assets/img/logo.png';
	import { t } from '$lib/i18n/index.svelte';
	import Form from './_Form.svelte';
	import { fake, type LoginState } from './_variants';

	let { variant, state }: { variant: string; state: LoginState } = $props();
</script>

<div class="screen">
	{#if variant === 'a'}
		<div class="a-wrap">
			<div class="a-card">
				<div class="brand"><img src={logo} alt="" /><span>{t('app.name')}</span></div>
				<Form {state} />
			</div>
			<p class="a-foot"><span class="mono">{fake.host}</span></p>
		</div>
	{:else if variant === 'b'}
		<div class="b-wrap">
			<div class="b-card">
				<aside class="b-side">
					<img src={logo} alt="" class="b-logo" />
					<div class="b-name">{t('app.name')}</div>
					<dl>
						<dt>Instance</dt>
						<dd class="mono">{fake.host}</dd>
						<dt>Version</dt>
						<dd class="mono">{fake.version}</dd>
						<dt>Platforms</dt>
						<dd>Twitch, Kick</dd>
					</dl>
				</aside>
				<div class="b-main"><Form {state} ssoFirst /></div>
			</div>
		</div>
	{:else}
		<div class="c-page">
			<header class="c-top">
				<div class="brand"><img src={logo} alt="" /><span>{t('app.name')}</span></div>
			</header>
			<main class="c-main"><Form {state} size="lg" /></main>
			<footer class="c-foot">
				<span class="dot" aria-hidden="true"></span><span>Miner reachable</span>
				<span class="sep" aria-hidden="true"></span>
				<span class="mono">{fake.host}</span>
				<span class="sep" aria-hidden="true"></span>
				<span class="mono">{fake.version}</span>
			</footer>
		</div>
	{/if}
</div>

<style>
	.screen {
		container-type: inline-size;
		height: 100%;
		background: var(--bg);
		color: var(--text);
		font: 14px/1.5 var(--font);
	}
	.screen :global(::selection) {
		background: var(--accent-soft);
		color: var(--text);
	}
	.mono {
		font-family: var(--mono);
		font-size: 12px;
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

	/* A: card */
	.a-wrap {
		min-height: 100%;
		display: flex;
		flex-direction: column;
		align-items: center;
		padding: 72px 16px 24px;
	}
	.a-card {
		width: 100%;
		max-width: 360px;
		background: var(--surface);
		border: 1px solid var(--border-soft);
		border-radius: 12px;
		padding: 24px;
	}
	.a-card .brand {
		margin-bottom: 20px;
	}
	.a-foot {
		margin: 14px 0 0;
		color: var(--t3);
	}

	/* B: split */
	.b-wrap {
		min-height: 100%;
		display: grid;
		place-items: start center;
		padding: 64px 16px 24px;
	}
	.b-card {
		width: 100%;
		max-width: 640px;
		display: grid;
		grid-template-columns: 220px minmax(0, 1fr);
		background: var(--surface);
		border: 1px solid var(--border-soft);
		border-radius: 12px;
		overflow: hidden;
	}
	.b-side {
		background: var(--surface-2);
		border-right: 1px solid var(--border-soft);
		padding: 24px;
	}
	.b-logo {
		width: 44px;
		height: 44px;
		border-radius: 10px;
		display: block;
	}
	.b-name {
		margin-top: 12px;
		font-size: 16px;
		font-weight: 600;
		letter-spacing: -0.01em;
	}
	dl {
		margin: 20px 0 0;
		display: grid;
		gap: 2px;
	}
	dt {
		font-size: 12px;
		color: var(--t3);
	}
	dt:not(:first-child) {
		margin-top: 8px;
	}
	dd {
		margin: 0;
		color: var(--t2);
		font-size: 13px;
	}
	.b-main {
		padding: 28px 28px 32px;
	}
	@container (max-width: 600px) {
		.b-card {
			grid-template-columns: minmax(0, 1fr);
			max-width: 360px;
		}
		.b-side {
			border-right: 0;
			border-bottom: 1px solid var(--border-soft);
			padding: 16px 20px;
			display: flex;
			align-items: center;
			gap: 10px;
		}
		.b-logo {
			width: 28px;
			height: 28px;
			border-radius: 6px;
		}
		.b-name {
			margin: 0;
			font-size: 14px;
		}
		dl {
			display: none;
		}
		.b-main {
			padding: 20px;
		}
	}

	/* C: bare page */
	.c-page {
		min-height: 100%;
		display: grid;
		grid-template-rows: auto 1fr auto;
	}
	.c-top {
		height: 52px;
		display: flex;
		align-items: center;
		padding: 0 24px;
		border-bottom: 1px solid var(--border-soft);
	}
	.c-main {
		width: 100%;
		max-width: 360px;
		margin: 0 auto;
		padding: 80px 16px 32px;
	}
	.c-foot {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		justify-content: center;
		gap: 10px;
		padding: 14px 16px;
		color: var(--t3);
		font-size: 12px;
	}
	.dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--green);
	}
	.sep {
		width: 1px;
		height: 12px;
		background: var(--border);
	}
</style>
