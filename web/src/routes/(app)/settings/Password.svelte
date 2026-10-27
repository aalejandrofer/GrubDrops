<script lang="ts">
	import { api } from '$lib/app';
	import { ApiError } from '$lib/api/client';
	import { t } from '$lib/i18n/index.svelte';
	import { passwordErrorKey, passwordProblem } from '$lib/settings';

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

<section class="set" id="password" aria-labelledby="h-pw">
	<div class="sec-head">
		<div>
			<h2 id="h-pw">{t('settings.password')}</h2>
			<p>{t('settings.password_sub')}</p>
		</div>
	</div>
	<form class="panel" onsubmit={changePassword}>
		<div class="card-body">
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
</section>
