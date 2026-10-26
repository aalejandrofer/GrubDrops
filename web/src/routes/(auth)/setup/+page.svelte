<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { api, session } from '$lib/app';
	import { ApiError, Unreachable } from '$lib/api/client';
	import { t } from '$lib/i18n/index.svelte';
	import '../auth.css';

	let password = $state('');
	let confirm = $state('');
	let error = $state('');
	let busy = $state(false);

	onMount(async () => {
		try {
			await session.load();
		} catch {
			error = t('auth.unreachable');
			return;
		}
		if (!session.setupNeeded) void goto(session.authed ? '/' : '/login');
	});

	async function submit(e: SubmitEvent): Promise<void> {
		e.preventDefault();
		if (busy) return;
		error = '';
		if (Array.from(password).length < 8) {
			error = t('auth.too_short');
			return;
		}
		if (password !== confirm) {
			error = t('auth.mismatch');
			return;
		}
		busy = true;
		try {
			await api.setup(password, confirm);
			await session.load();
			await goto('/');
		} catch (err) {
			if (err instanceof Unreachable) error = t('auth.unreachable');
			else if (err instanceof ApiError && err.body.message) error = err.body.message;
			else error = t('auth.failed');
		} finally {
			busy = false;
		}
	}
</script>

<h1>{t('auth.setup_title')}</h1>
<form onsubmit={submit} novalidate>
	<label for="pw">{t('auth.password')}</label>
	<!-- svelte-ignore a11y_autofocus -->
	<input id="pw" type="password" autocomplete="new-password" autofocus bind:value={password} />
	<label for="pw2">{t('auth.confirm')}</label>
	<input id="pw2" type="password" autocomplete="new-password" bind:value={confirm} />
	{#if error}<p class="err" role="alert">{error}</p>{/if}
	<button type="submit" class="primary" disabled={busy}>{t('auth.setup_submit')}</button>
</form>
