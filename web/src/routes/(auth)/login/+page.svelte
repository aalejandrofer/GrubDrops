<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { api, session } from '$lib/app';
	import { ApiError, Unreachable } from '$lib/api/client';
	import { t } from '$lib/i18n/index.svelte';
	import '../auth.css';

	let password = $state('');
	let error = $state('');
	let busy = $state(false);
	const ssoFailed = $derived(page.url.searchParams.get('login_error') === 'oidc');

	onMount(async () => {
		try {
			await session.load();
		} catch {
			error = t('auth.unreachable');
			return;
		}
		if (session.setupNeeded) void goto('/setup');
		else if (session.authed) void goto('/');
	});

	async function submit(e: SubmitEvent): Promise<void> {
		e.preventDefault();
		if (busy) return;
		busy = true;
		error = '';
		try {
			await api.login(password);
			await session.load();
			await goto('/');
		} catch (err) {
			if (err instanceof ApiError && err.status === 401) error = t('auth.wrong_password');
			else if (err instanceof Unreachable) error = t('auth.unreachable');
			else error = t('auth.failed');
		} finally {
			busy = false;
		}
	}
</script>

<h1>{t('auth.login_title')}</h1>
{#if ssoFailed}<p class="banner" role="alert">{t('auth.sso_failed')}</p>{/if}
<form onsubmit={submit}>
	<label for="pw">{t('auth.password')}</label>
	<!-- svelte-ignore a11y_autofocus -->
	<input id="pw" type="password" autocomplete="current-password" autofocus required bind:value={password} aria-invalid={error ? true : undefined} />
	{#if error}<p class="err" role="alert">{error}</p>{/if}
	<button type="submit" class="primary" disabled={busy}>{t('auth.login_submit')}</button>
</form>
{#if session.oidc.enabled}
	<a class="sso" href="/auth/oidc/login">{t('auth.sso', { name: session.oidc.name })}</a>
{/if}
