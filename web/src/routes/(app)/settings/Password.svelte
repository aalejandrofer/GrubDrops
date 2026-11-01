<script lang="ts">
	import NewPill from '$lib/components/NewPill.svelte';
	import { api } from '$lib/app';
	import { ApiError } from '$lib/api/client';
	import type { Settings, SettingsPatch } from '$lib/api/types';
	import { t } from '$lib/i18n/index.svelte';
	import { passwordErrorKey, passwordProblem } from '$lib/settings';
	import { SECTION_LOOK, passwordSwitch, ssoLine } from '$lib/settings-look';
	import OidcGuide from './OidcGuide.svelte';
	import SectionHead from './SectionHead.svelte';

	let {
		s,
		save,
		errs = {}
	}: {
		/** Settings for the sign-on rows; they hide until they load. */
		s?: Settings;
		save?: (key: string, p: SettingsPatch) => Promise<boolean>;
		errs?: Record<string, boolean>;
	} = $props();

	let guide = $state(false);
	const pw = $derived(s ? passwordSwitch(s) : null);

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
</script>

<section class="set hue-{SECTION_LOOK.password.hue}" id="password" aria-labelledby="h-pw">
	<SectionHead sec="password" id="h-pw" blurb={t('settings.auth_sub')} />
	<div class="panel">
		{#if s && pw && save}
			<div class="row">
				<div class="lbl">
					<b class="with-chip"
						>{t('settings.sso')}
						{#if s.sso.enabled}<span class="chip ok">{s.sso.name}</span>{:else}<span class="chip off">{t('settings.sso_off')}</span>{/if}</b
					>
					<small>{ssoLine(s.sso)}</small>
				</div>
				{#if !s.sso.enabled}<button type="button" class="btn" onclick={() => (guide = true)}>{t('settings.sso_setup')}</button>{/if}
			</div>
			<div class="row">
				<div class="lbl">
					<b id="pwl-l">{t('settings.password_login')}<NewPill since="2.3" /></b>
					<small>{pw.hint}</small>
					{#if errs.password_login_disabled}<small class="bad">{t('settings.save_failed')}</small>{/if}
				</div>
				{#if pw.toggle}
					<button
						type="button"
						class="sw"
						role="switch"
						aria-checked={pw.on}
						aria-labelledby="pwl-l"
						onclick={() => save('password_login_disabled', { password_login_disabled: pw.on })}
					></button>
				{:else}
					<span class="chip off">{t('settings.oidc_off')}</span>
				{/if}
			</div>
		{/if}
		<form class="card-body pwform" onsubmit={changePassword}>
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
		</form>
	</div>
	<OidcGuide bind:open={guide} />
</section>

<style>
	.lbl b.with-chip {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 2px 8px;
	}
	/* Keep the password fields a readable width on wide screens. */
	.card-body.pwform {
		grid-template-columns: minmax(0, 560px);
	}
</style>
