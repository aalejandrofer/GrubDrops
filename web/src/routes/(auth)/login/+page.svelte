<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { api, session } from '$lib/app';
	import { ApiError, Unreachable } from '$lib/api/client';
	import { retryUnreachable } from '$lib/retry';
	import { t } from '$lib/i18n/index.svelte';

	let password = $state('');
	let error = $state('');
	let busy = $state(false);
	let down = $state(false);
	const shown = $derived(error || (down ? t('auth.unreachable') : ''));
	const ssoFailed = $derived(page.url.searchParams.get('login_error') === 'oidc');

	onMount(() => {
		const r = retryUnreachable(() => session.load(), { onState: (d) => (down = d) });
		void r.done.then((ok) => {
			if (!ok) return;
			if (session.setupNeeded) void goto('/setup');
			else if (session.authed) void goto('/');
		})
			.catch(() => {});
		return r.stop;
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
	<input
		id="pw"
		type="password"
		autocomplete="current-password"
		autofocus
		required
		readonly={busy}
		bind:value={password}
		aria-invalid={shown ? true : undefined}
		aria-describedby={shown ? 'pw-err' : undefined}
	/>
	{#if shown}<p class="err" id="pw-err" role="alert">{shown}</p>{/if}
	<button type="submit" class="b primary" disabled={busy}>
		{#if busy}<span class="spin" aria-hidden="true"></span>{t('auth.signing_in')}{:else}{t('auth.login_submit')}{/if}
	</button>
</form>
{#if session.oidc.enabled}
	<div class="rule"><span>{t('auth.or')}</span></div>
	<a class="b ghost" href="/auth/oidc/login">
		<svg viewBox="0 0 24 24" aria-hidden="true"
			><path
				d="M15 7a3 3 0 1 1-3 3M14.2 12.8 21 19.6V22h-3v-2h-2v-2h-2l-2.2-2.2A6 6 0 1 1 15 7"
				fill="none"
				stroke="currentColor"
				stroke-width="1.7"
				stroke-linecap="round"
				stroke-linejoin="round"
			/></svg
		>{t('auth.sso', { name: session.oidc.name })}
	</a>
{/if}
