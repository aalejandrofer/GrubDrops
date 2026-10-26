<script lang="ts">
	import { t } from '$lib/i18n/index.svelte';
	import { fake, type LoginState } from './_variants';

	let {
		state,
		ssoFirst = false,
		size = 'md'
	}: { state: LoginState; ssoFirst?: boolean; size?: 'md' | 'lg' } = $props();

	const uid = $props.id();
	const filled = $derived(state === 'error' || state === 'loading');
	const busy = $derived(state === 'loading');
	const sso = t('auth.sso', { name: fake.provider });
</script>

{#snippet ssoButton(primary: boolean)}
	<a class="b" class:primary class:ghost={!primary} href="#sso" aria-disabled={busy || undefined}>
		<svg viewBox="0 0 24 24" aria-hidden="true"
			><path d="M15 7a3 3 0 1 1-3 3M14.2 12.8 21 19.6V22h-3v-2h-2v-2h-2l-2.2-2.2A6 6 0 1 1 15 7" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" /></svg
		>{sso}
	</a>
{/snippet}

{#snippet passwordForm(primary: boolean)}
	<form class="f" onsubmit={(e) => e.preventDefault()}>
		<label for={`${uid}-pw`}>{t('auth.password')}</label>
		<input
			id={`${uid}-pw`}
			type="password"
			autocomplete="current-password"
			value={filled ? 'correct-horse' : ''}
			disabled={busy}
			aria-invalid={state === 'error' || undefined}
			aria-describedby={state === 'error' ? `${uid}-err` : undefined}
		/>
		{#if state === 'error'}<p class="err" id={`${uid}-err`} role="alert">{t('auth.wrong_password')}</p>{/if}
		<button type="submit" class="b" class:primary class:ghost={!primary} disabled={busy}>
			{#if busy}<span class="spin" aria-hidden="true"></span>Signing in{:else}{t('auth.login_submit')}{/if}
		</button>
	</form>
{/snippet}

<div class="form {size}">
	{#if state === 'setup'}
		<h1>{t('auth.setup_title')}</h1>
		<p class="lead">Create the admin password for this install.</p>
		<form class="f" onsubmit={(e) => e.preventDefault()}>
			<label for={`${uid}-n1`}>{t('auth.password')}</label>
			<input id={`${uid}-n1`} type="password" autocomplete="new-password" aria-describedby={`${uid}-hint`} />
			<p class="hint" id={`${uid}-hint`}>{t('auth.too_short')}</p>
			<label for={`${uid}-n2`}>{t('auth.confirm')}</label>
			<input id={`${uid}-n2`} type="password" autocomplete="new-password" />
			<button type="submit" class="b primary">{t('auth.setup_submit')}</button>
		</form>
	{:else if state === 'sso'}
		<h1>{t('auth.login_title')}</h1>
		<div class="stack">{@render ssoButton(true)}</div>
		<p class="hint">Password sign-in is off on this install.</p>
	{:else if ssoFirst}
		<h1>{t('auth.login_title')}</h1>
		<div class="stack">{@render ssoButton(true)}</div>
		<div class="rule"><span>or use the password</span></div>
		{@render passwordForm(false)}
	{:else}
		<h1>{t('auth.login_title')}</h1>
		{@render passwordForm(true)}
		<div class="rule"><span>or</span></div>
		<div class="stack">{@render ssoButton(false)}</div>
	{/if}
</div>

<style>
	.form {
		--h: 36px;
		--fs: 14px;
	}
	.form.lg {
		--h: 40px;
	}
	h1 {
		margin: 0 0 16px;
		font-size: 18px;
		font-weight: 600;
		letter-spacing: -0.01em;
		text-wrap: balance;
	}
	.lg h1 {
		font-size: 24px;
		letter-spacing: -0.02em;
		margin-bottom: 20px;
	}
	.lead {
		margin: -10px 0 16px;
		color: var(--t2);
		font-size: 13px;
	}
	.lg .lead {
		margin-top: -14px;
	}
	.f {
		display: grid;
		gap: 6px;
	}
	label {
		font-size: 12px;
		color: var(--t2);
	}
	label:not(:first-child) {
		margin-top: 6px;
	}
	input {
		font: inherit;
		color: var(--text);
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: 6px;
		height: var(--h);
		padding: 0 10px;
		width: 100%;
		caret-color: var(--accent);
	}
	input:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	input:disabled {
		color: var(--t3);
		background: var(--surface-2);
	}
	input[aria-invalid='true'] {
		border-color: var(--red);
	}
	.err {
		margin: 0;
		font-size: 12px;
		color: var(--red);
	}
	.hint {
		margin: 0;
		font-size: 12px;
		color: var(--t3);
	}
	.stack + .hint {
		margin-top: 10px;
	}
	.b {
		margin-top: 8px;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: 8px;
		height: var(--h);
		font: inherit;
		font-weight: 500;
		border-radius: 6px;
		padding: 0 12px;
		cursor: pointer;
		text-decoration: none;
		width: 100%;
	}
	.stack .b {
		margin-top: 0;
	}
	.b svg {
		width: 15px;
		height: 15px;
		flex: none;
	}
	.primary {
		color: #fff;
		background: var(--accent-strong);
		border: 1px solid var(--accent-strong);
	}
	.primary:hover:not(:disabled) {
		background: var(--accent);
		border-color: var(--accent);
	}
	.ghost {
		color: var(--text);
		background: transparent;
		border: 1px solid var(--border);
	}
	.ghost:hover:not(:disabled) {
		border-color: var(--t2);
	}
	.b:disabled,
	.b[aria-disabled='true'] {
		opacity: 0.7;
		cursor: default;
	}
	.b:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.rule {
		display: flex;
		align-items: center;
		gap: 10px;
		margin: 16px 0 8px;
		color: var(--t3);
		font-size: 12px;
	}
	.rule::before,
	.rule::after {
		content: '';
		flex: 1;
		height: 1px;
		background: var(--border-soft);
	}
	.spin {
		width: 13px;
		height: 13px;
		border-radius: 50%;
		border: 2px solid color-mix(in srgb, currentColor 30%, transparent);
		border-top-color: currentColor;
		animation: spin 0.8s linear infinite;
	}
	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.spin {
			animation-duration: 2.4s;
		}
	}
</style>
