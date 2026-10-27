<script lang="ts">
	import { api } from '$lib/app';
	import { ApiError } from '$lib/api/client';
	import type { Settings, SettingsPatch } from '$lib/api/types';
	import { t } from '$lib/i18n/index.svelte';
	import { SCAN_INTERVALS, isProxyURL, minutesLabel } from '$lib/settings';

	let {
		s,
		save,
		errs
	}: {
		s: Settings;
		save: (key: string, p: SettingsPatch) => Promise<boolean>;
		errs: Record<string, boolean>;
	} = $props();

	const scanOptions = $derived(SCAN_INTERVALS.includes(s.scan_interval_min) ? SCAN_INTERVALS : [...SCAN_INTERVALS, s.scan_interval_min]);

	const KICK_MODES = [
		{ id: 'auto', key: 'settings.kick_auto', sub: 'settings.kick_auto_sub' },
		{ id: 'browser', key: 'settings.kick_browser', sub: 'settings.kick_browser_sub' },
		{ id: 'ws', key: 'settings.kick_ws', sub: 'settings.kick_ws_sub' }
	] as const;
	const kickSub = $derived(KICK_MODES.find((m) => m.id === s.kick_watch_mode)?.sub ?? 'settings.kick_auto_sub');

	// The field starts as the saved URL with the password hidden; sending that
	// hint back leaves the saved URL alone.
	let proxyURL = $state('');
	let proxyDirty = $state(false);
	let proxyBad = $state(false);
	let proxyTesting = $state(false);
	let proxyFeedback = $state<{ ok: boolean; text: string } | null>(null);
	$effect(() => {
		if (!proxyDirty) proxyURL = s.proxy.url;
	});

	async function saveProxy(): Promise<void> {
		const v = proxyURL.trim();
		if (!isProxyURL(v)) {
			proxyBad = true;
			return;
		}
		proxyFeedback = null;
		const patch: SettingsPatch = { proxy_url: v };
		if (v === '') patch.proxy_enabled = false;
		if (await save('proxy_url', patch)) proxyDirty = false;
	}

	async function testProxy(): Promise<void> {
		const v = proxyURL.trim();
		if (!isProxyURL(v) || v === '') {
			proxyBad = v !== '';
			return;
		}
		proxyTesting = true;
		proxyFeedback = null;
		try {
			const r = await api.proxyTest(v);
			proxyFeedback = { ok: true, text: t('settings.proxy_test_ok', { ms: r.ms }) };
		} catch (e) {
			proxyFeedback = {
				ok: false,
				text: e instanceof ApiError && e.status === 502 ? t('settings.proxy_test_failed') : t('settings.test_failed')
			};
		} finally {
			proxyTesting = false;
		}
	}
</script>

<section class="set" id="engine" aria-labelledby="h-eng">
	<div class="sec-head"><div><h2 id="h-eng">{t('settings.engine')}</h2></div></div>
	<div class="panel">
		<div class="row">
			<div class="lbl">
				<b id="pm-l">{t('settings.priority_mode')}</b>
				<small>{s.priority_mode === 'ending_soonest' ? t('settings.pm_ending_soonest_sub') : t('settings.pm_ordered_sub')}</small>
				{#if errs.priority_mode}<small class="bad">{t('settings.save_failed')}</small>{/if}
			</div>
			<div class="seg" role="group" aria-labelledby="pm-l">
				<button type="button" aria-pressed={s.priority_mode === 'ordered'} onclick={() => save('priority_mode', { priority_mode: 'ordered' })}>
					{t('settings.pm_ordered')}
				</button>
				<button type="button" aria-pressed={s.priority_mode === 'ending_soonest'} onclick={() => save('priority_mode', { priority_mode: 'ending_soonest' })}>
					{t('settings.pm_ending_soonest')}
				</button>
			</div>
		</div>
		<div class="row">
			<div class="lbl">
				<b id="pre-l">{t('settings.preempt')}</b>
				<small>{s.preempt ? t('settings.preempt_on_sub') : t('settings.preempt_off_sub')}</small>
				{#if errs.preempt}<small class="bad">{t('settings.save_failed')}</small>{/if}
			</div>
			<button
				type="button"
				class="sw"
				role="switch"
				aria-checked={s.preempt}
				aria-labelledby="pre-l"
				onclick={() => save('preempt', { preempt: !s.preempt })}
			></button>
		</div>
		<div class="row">
			<div class="lbl">
				<b><label for="scan">{t('settings.scan_interval')}</label></b>
				<small>{t('settings.scan_interval_sub')}</small>
				{#if errs.scan_interval_min}<small class="bad">{t('settings.save_failed')}</small>{/if}
			</div>
			<select
				class="sel"
				id="scan"
				autocomplete="off"
				value={s.scan_interval_min}
				onchange={(e) => {
					const v = Number(e.currentTarget.value);
					if (e.isTrusted && v !== s.scan_interval_min) void save('scan_interval_min', { scan_interval_min: v });
				}}
			>
				{#each scanOptions as m (m)}<option value={m}>{minutesLabel(m)}</option>{/each}
			</select>
		</div>
	</div>

	<details class="adv">
		<summary>{t('settings.advanced')}</summary>
		<div class="panel">
			<div class="row">
				<div class="lbl">
					<b id="kw-l">{t('settings.kick_watch_mode')}</b>
					<small>{t(kickSub)} {t('settings.after_restart')}</small>
					{#if errs.kick_watch_mode}<small class="bad">{t('settings.save_failed')}</small>{/if}
				</div>
				<div class="seg" role="group" aria-labelledby="kw-l">
					{#each KICK_MODES as m (m.id)}
						<button type="button" aria-pressed={s.kick_watch_mode === m.id} onclick={() => save('kick_watch_mode', { kick_watch_mode: m.id })}>
							{t(m.key)}
						</button>
					{/each}
				</div>
			</div>
		</div>

		<h3 class="sub-h">{t('settings.proxy')}</h3>
		<div class="panel">
			<div class="row">
				<div class="lbl">
					<b id="px-l">{t('settings.proxy_use')}</b>
					<small>{t('settings.proxy_sub')}</small>
					{#if errs.proxy_enabled}<small class="bad">{t('settings.save_failed')}</small>{/if}
				</div>
				<button
					type="button"
					class="sw"
					role="switch"
					aria-checked={s.proxy.enabled}
					aria-labelledby="px-l"
					disabled={!s.proxy.enabled && s.proxy.url === ''}
					onclick={() => save('proxy_enabled', { proxy_enabled: !s.proxy.enabled })}
				></button>
			</div>
			<div class="card-body">
				<div class="field">
					<label for="proxy-url">{t('settings.proxy_url')}</label>
					<div class="inline wrap">
						<input
							class="inp"
							id="proxy-url"
							name="proxy-url"
							type="text"
							inputmode="url"
							autocomplete="off"
							spellcheck="false"
							data-1p-ignore
							data-lpignore="true"
							data-bwignore
							placeholder="socks5://user:pass@127.0.0.1:1080"
							aria-invalid={proxyBad ? true : undefined}
							bind:value={proxyURL}
							oninput={() => ((proxyDirty = true), (proxyBad = false), (proxyFeedback = null))}
							onkeydown={(e) => {
								if (e.key === 'Enter' && e.isTrusted && proxyDirty) void saveProxy();
							}}
						/>
						<button type="button" class="btn" disabled={!proxyDirty} onclick={saveProxy}>{t('accounts.save')}</button>
						<button type="button" class="btn" disabled={proxyTesting || proxyURL.trim() === ''} onclick={testProxy}>
							{proxyTesting ? t('settings.proxy_testing') : t('settings.proxy_test')}
						</button>
					</div>
					<small class="help">{t('settings.proxy_help')}</small>
					{#if proxyBad}<span class="feedback bad" role="alert">{t('settings.proxy_invalid')}</span>{/if}
					{#if errs.proxy_url}<span class="feedback bad">{t('settings.save_failed')}</span>{/if}
					{#if proxyFeedback}<span class="feedback" class:bad={!proxyFeedback.ok} role="status">{proxyFeedback.text}</span>{/if}
				</div>
			</div>
		</div>
	</details>
</section>

<style>
	.bad {
		color: var(--red);
	}
	.help {
		color: var(--t2);
		font-size: 12px;
	}
	.inp {
		flex: 1;
		min-width: 0;
	}
</style>
