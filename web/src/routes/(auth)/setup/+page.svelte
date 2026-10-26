<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { api, session } from '$lib/app';
	import { ApiError, Unreachable } from '$lib/api/client';
	import { retryUnreachable } from '$lib/retry';
	import { t } from '$lib/i18n/index.svelte';

	let password = $state('');
	let confirm = $state('');
	let error = $state('');
	let short = $state(false);
	let busy = $state(false);
	let down = $state(false);
	const shown = $derived(error || (down ? t('auth.unreachable') : ''));

	onMount(() => {
		const r = retryUnreachable(() => session.load(), { onState: (d) => (down = d) });
		void r.done.then((ok) => {
			if (!ok) return;
			if (!session.setupNeeded) void goto(session.authed ? '/' : '/login');
		})
			.catch(() => {});
		return r.stop;
	});

	async function submit(e: SubmitEvent): Promise<void> {
		e.preventDefault();
		if (busy) return;
		error = '';
		short = Array.from(password).length < 8;
		if (short) return;
		if (password !== confirm) {
			error = t('auth.mismatch');
			return;
		}
		busy = true;
		try {
			await api.setup(password, confirm);
			await session.load();
			await goto('/welcome');
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
<p class="lead">{t('auth.setup_lead')}</p>
<form onsubmit={submit} novalidate>
	<label for="pw">{t('auth.password')}</label>
	<!-- svelte-ignore a11y_autofocus -->
	<input
		id="pw"
		type="password"
		autocomplete="new-password"
		autofocus
		readonly={busy}
		bind:value={password}
		aria-invalid={short || undefined}
		aria-describedby="pw-hint"
		oninput={() => (short = false)}
	/>
	<p class="hint" class:err={short} id="pw-hint" role={short ? 'alert' : undefined}>{t('auth.too_short')}</p>
	<label for="pw2">{t('auth.confirm')}</label>
	<input
		id="pw2"
		type="password"
		autocomplete="new-password"
		readonly={busy}
		bind:value={confirm}
		aria-invalid={error === t('auth.mismatch') || undefined}
		aria-describedby={shown ? 'pw-err' : undefined}
	/>
	{#if shown}<p class="err" id="pw-err" role="alert">{shown}</p>{/if}
	<button type="submit" class="b primary" disabled={busy}>
		{#if busy}<span class="spin" aria-hidden="true"></span>{t('auth.setup_busy')}{:else}{t('auth.setup_submit')}{/if}
	</button>
</form>
