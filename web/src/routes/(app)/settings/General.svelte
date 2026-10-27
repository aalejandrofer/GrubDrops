<script lang="ts">
	import { goto } from '$app/navigation';
	import { api } from '$lib/app';
	import { ApiError } from '$lib/api/client';
	import type { Settings, SettingsPatch } from '$lib/api/types';
	import { LANGS, t, type Lang } from '$lib/i18n/index.svelte';
	import { prefs } from '$lib/prefs.svelte';
	import { passwordErrorKey, passwordProblem, timeExample, zoneOptions } from '$lib/settings';

	let {
		s,
		save,
		errs
	}: {
		s: Settings;
		save: (key: string, p: SettingsPatch) => Promise<boolean>;
		errs: Record<string, boolean>;
	} = $props();

	const now = Date.now();
	const example = $derived(timeExample(s.time_format, prefs.zone, now));
	const zones = $derived.by(() => {
		const o = zoneOptions(s.zone_effective ?? 'UTC');
		if (s.timezone && !o.some((z) => z.value === s.timezone)) o.push({ value: s.timezone, label: s.timezone });
		return o;
	});

	let signoutFailed = $state(false);

	async function signOut(): Promise<void> {
		signoutFailed = false;
		try {
			await api.logout();
		} catch {
			signoutFailed = true;
			return;
		}
		await goto('/login');
	}

	let pwCurrent = $state('');
	let pwNew = $state('');
	let pwConfirm = $state('');
	let pwBusy = $state(false);
	let pwFeedback = $state<{ ok: boolean; text: string } | null>(null);

	async function changePassword(e: SubmitEvent): Promise<void> {
		e.preventDefault();
		if (pwBusy) return;
		const problem = passwordProblem(pwNew, pwConfirm);
		if (problem) {
			pwFeedback = { ok: false, text: t(problem) };
			return;
		}
		pwBusy = true;
		pwFeedback = null;
		try {
			await api.changePassword(pwCurrent, pwNew);
			pwCurrent = pwNew = pwConfirm = '';
			pwFeedback = { ok: true, text: t('settings.password_saved') };
		} catch (err) {
			pwFeedback = { ok: false, text: t(passwordErrorKey(err instanceof ApiError ? err.code : undefined)) };
		} finally {
			pwBusy = false;
		}
	}

	let snapBusy = $state(false);
	let snapFailed = $state(false);

	async function downloadSnapshot(): Promise<void> {
		snapBusy = true;
		snapFailed = false;
		try {
			const { blob, name } = await api.snapshot();
			const url = URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = name;
			a.click();
			setTimeout(() => URL.revokeObjectURL(url), 1000);
		} catch {
			snapFailed = true;
		} finally {
			snapBusy = false;
		}
	}
</script>

<section class="set" id="general" aria-labelledby="h-gen">
	<div class="sec-head"><div><h2 id="h-gen">{t('settings.general')}</h2></div></div>
	<div class="panel">
		<div class="row">
			<div class="lbl">
				<b><label for="lang">{t('settings.language')}</label></b>
				{#if errs.language}<small class="bad">{t('settings.save_failed')}</small>{/if}
			</div>
			<select
				class="sel"
				id="lang"
				autocomplete="off"
				value={s.language}
				onchange={(e) => {
					const v = e.currentTarget.value as Lang;
					if (e.isTrusted && v !== s.language) void save('language', { language: v });
				}}
			>
				{#each LANGS as l (l.code)}<option value={l.code}>{l.label}</option>{/each}
			</select>
		</div>
		<div class="row">
			<div class="lbl">
				<b id="tf-l">{t('settings.time_format')}</b>
				<small>{example}</small>
				{#if errs.time_format}<small class="bad">{t('settings.save_failed')}</small>{/if}
			</div>
			<div class="seg" role="group" aria-labelledby="tf-l">
				<button type="button" aria-pressed={s.time_format === '24h'} onclick={() => save('time_format', { time_format: '24h' })}>24h</button>
				<button type="button" aria-pressed={s.time_format === '12h'} onclick={() => save('time_format', { time_format: '12h' })}>12h</button>
			</div>
		</div>
		<div class="row">
			<div class="lbl">
				<b id="th-l">{t('settings.theme')}</b>
				{#if errs.theme}<small class="bad">{t('settings.save_failed')}</small>{/if}
			</div>
			<div class="seg" role="group" aria-labelledby="th-l">
				<button type="button" aria-pressed={s.theme === 'dark'} onclick={() => save('theme', { theme: 'dark' })}>{t('settings.theme_dark')}</button>
				<button type="button" aria-pressed={s.theme === 'light'} onclick={() => save('theme', { theme: 'light' })}>{t('settings.theme_light')}</button>
			</div>
		</div>
		<div class="row">
			<div class="lbl">
				<b><label for="tz">{t('settings.timezone')}</label></b>
				{#if errs.timezone}<small class="bad">{t('settings.save_failed')}</small>{/if}
			</div>
			<select
				class="sel tz"
				id="tz"
				autocomplete="off"
				value={s.timezone}
				onchange={(e) => {
					const v = e.currentTarget.value;
					if (e.isTrusted && v !== s.timezone) void save('timezone', { timezone: v });
				}}
			>
				{#each zones as z (z.value)}<option value={z.value}>{z.label}</option>{/each}
			</select>
		</div>
		<div class="row">
			<div class="lbl">
				{#if signoutFailed}<small class="bad">{t('settings.signout_failed')}</small>{/if}
			</div>
			<button type="button" class="btn" onclick={signOut}>{t('settings.sign_out')}</button>
		</div>
	</div>

	<h3 class="sub-h">{t('settings.password')}</h3>
	<form class="panel" onsubmit={changePassword}>
		<div class="card-body">
			<small class="help">{t('settings.password_sub')}</small>
			<div class="field">
				<label for="pw-cur">{t('settings.password_current')}</label>
				<input class="inp" id="pw-cur" name="current-password" type="password" autocomplete="current-password" required bind:value={pwCurrent} />
			</div>
			<div class="two">
				<div class="field">
					<label for="pw-new">{t('settings.password_new')}</label>
					<input class="inp" id="pw-new" name="new-password" type="password" autocomplete="new-password" required bind:value={pwNew} />
				</div>
				<div class="field">
					<label for="pw-conf">{t('settings.password_confirm')}</label>
					<input class="inp" id="pw-conf" name="confirm-password" type="password" autocomplete="new-password" required bind:value={pwConfirm} />
				</div>
			</div>
			<div class="inline wrap">
				<button type="submit" class="btn" disabled={pwBusy || pwCurrent === '' || pwNew === ''}>
					{pwBusy ? t('settings.password_saving') : t('settings.password_save')}
				</button>
				{#if pwFeedback}<span class="feedback" class:bad={!pwFeedback.ok} role="status">{pwFeedback.text}</span>{/if}
			</div>
		</div>
	</form>

	<h3 class="sub-h">{t('settings.backup')}</h3>
	<div class="panel">
		<div class="row">
			<div class="lbl">
				<b>{t('settings.snapshot')}</b>
				<small>{t('settings.snapshot_sub')}</small>
				{#if snapFailed}<small class="bad" role="alert">{t('settings.snapshot_failed')}</small>{/if}
			</div>
			<button type="button" class="btn" disabled={snapBusy} onclick={downloadSnapshot}>
				{snapBusy ? t('settings.snapshot_busy') : t('settings.snapshot_download')}
			</button>
		</div>
	</div>
</section>

<style>
	.tz {
		max-width: 260px;
	}
	.bad {
		color: var(--red);
	}
	.help {
		color: var(--t2);
		font-size: 12px;
	}
</style>
