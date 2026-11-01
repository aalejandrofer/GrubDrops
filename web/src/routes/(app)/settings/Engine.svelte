<script lang="ts">
	import { tick } from 'svelte';
	import Hint, { type HintExample } from '$lib/components/Hint.svelte';
	import NewPill from '$lib/components/NewPill.svelte';
	import { SECTION_LOOK } from '$lib/settings-look';
	import { api } from '$lib/app';
	import { ApiError } from '$lib/api/client';
	import type { Settings, SettingsPatch } from '$lib/api/types';
	import { typed, type Autosave } from '$lib/autosave';
	import { withScheme } from '$lib/hint';
	import { t } from '$lib/i18n/index.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import { SCAN_INTERVALS, isProxyURL, minutesLabel, sleepProblem } from '$lib/settings';
	import SectionHead from './SectionHead.svelte';

	let {
		s,
		save,
		errs,
		auto
	}: {
		s: Settings;
		save: (key: string, p: SettingsPatch) => Promise<boolean>;
		errs: Record<string, boolean>;
		auto: Autosave;
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
	/** The switch needs a saved proxy URL. */
	const proxyNoURL = $derived(!s.proxy.enabled && s.proxy.url === '');
	$effect(() => {
		if (!proxyDirty) proxyURL = s.proxy.url;
	});

	async function saveProxy(): Promise<boolean> {
		const v = proxyURL.trim();
		if (!isProxyURL(v)) {
			proxyBad = true;
			return false;
		}
		proxyFeedback = null;
		const patch: SettingsPatch = { proxy_url: v };
		if (v === '') patch.proxy_enabled = false;
		const ok = await save('proxy_url', patch);
		if (ok) proxyDirty = false;
		return ok;
	}
	const proxyValid = (): boolean => isProxyURL(proxyURL.trim());

	let proxyInput = $state<HTMLInputElement>();
	const proxyExamples: HintExample[] = [{ label: 'http://' }, { label: 'https://' }, { label: 'socks5://' }];
	/** Puts the picked scheme on the URL. Not typing, so it waits for Save like autofill. */
	async function pickScheme(scheme: string): Promise<void> {
		proxyURL = withScheme(proxyURL, scheme);
		proxyDirty = true;
		proxyBad = false;
		proxyFeedback = null;
		await tick();
		proxyInput?.focus();
		proxyInput?.setSelectionRange(proxyURL.length, proxyURL.length);
	}

	// The URL can hold a password, so only typing starts a save, never autofill.
	function editProxy(e: Event): void {
		proxyDirty = true;
		proxyBad = false;
		proxyFeedback = null;
		if (typed(e as InputEvent)) auto.edit('proxy_url', saveProxy, proxyValid);
	}

	// Times save 10s after the last edit, or on Save; the switch saves at once.
	let sleepStart = $state('');
	let sleepEnd = $state('');
	let sleepDirty = $state(false);
	let sleepBad = $state<string | null>(null);
	$effect(() => {
		if (!sleepDirty) {
			sleepStart = s.sleep.start;
			sleepEnd = s.sleep.end;
		}
	});

	async function saveSleep(): Promise<boolean> {
		const problem = sleepProblem(sleepStart, sleepEnd);
		if (problem) {
			sleepBad = problem;
			return false;
		}
		const ok = await save('sleep_times', { sleep: { start: sleepStart, end: sleepEnd } });
		if (ok) sleepDirty = false;
		return ok;
	}
	const sleepValid = (): boolean => sleepProblem(sleepStart, sleepEnd) === null;

	function editSleep(): void {
		sleepDirty = true;
		sleepBad = null;
		auto.edit('sleep_times', saveSleep, sleepValid);
	}

	const saveSleepNow = (): Promise<boolean> => auto.now('sleep_times', saveSleep, sleepValid);
	const saveProxyNow = (): Promise<boolean> => auto.now('proxy_url', saveProxy, proxyValid);

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

<section class="set hue-{SECTION_LOOK.engine.hue}" id="engine" aria-labelledby="h-eng">
	<SectionHead sec="engine" id="h-eng" blurb={t('settings.engine_sub')} />
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
				<small>{!s.preempt ? t('settings.preempt_off_sub') : s.priority_mode === 'ending_soonest' ? t('settings.preempt_on_sub_ending') : t('settings.preempt_on_sub')}</small>
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
				<b id="raid-l">{t('settings.follow_raids')}<NewPill since="2.3" /></b>
				<small>{t('settings.follow_raids_sub')}</small>
				{#if errs.follow_raids}<small class="bad">{t('settings.save_failed')}</small>{/if}
			</div>
			<button
				type="button"
				class="sw"
				role="switch"
				aria-checked={s.follow_raids}
				aria-labelledby="raid-l"
				onclick={() => save('follow_raids', { follow_raids: !s.follow_raids })}
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

	<h3 class="sub-h"><span class="tile sm" aria-hidden="true"><Icon name="moon" size={13} /></span>{t('settings.sleep')}<NewPill since="2.3" /></h3>
	<div class="panel">
		<div class="row">
			<div class="lbl">
				<b id="sl-l">{t('settings.sleep_use')}</b>
				<small>{t('settings.sleep_sub')}</small>
				{#if errs.sleep_enabled}<small class="bad">{t('settings.save_failed')}</small>{/if}
			</div>
			<button
				type="button"
				class="sw"
				role="switch"
				aria-checked={s.sleep.enabled}
				aria-labelledby="sl-l"
				onclick={() => save('sleep_enabled', { sleep: { enabled: !s.sleep.enabled } })}
			></button>
		</div>
		<div class="card-body">
			<div class="inline wrap times">
				<div class="field">
					<label for="sl-start">{t('settings.sleep_start')}</label>
					<input
						class="inp"
						id="sl-start"
						type="time"
						autocomplete="off"
						aria-invalid={sleepBad ? true : undefined}
						bind:value={sleepStart}
						oninput={editSleep}
						onkeydown={(e) => {
							if (e.key === 'Enter' && e.isTrusted && sleepDirty) void saveSleepNow();
						}}
					/>
				</div>
				<div class="field">
					<label for="sl-end">{t('settings.sleep_end')}</label>
					<input
						class="inp"
						id="sl-end"
						type="time"
						autocomplete="off"
						aria-invalid={sleepBad ? true : undefined}
						bind:value={sleepEnd}
						oninput={editSleep}
						onkeydown={(e) => {
							if (e.key === 'Enter' && e.isTrusted && sleepDirty) void saveSleepNow();
						}}
					/>
				</div>
				<button type="button" class="btn" disabled={!sleepDirty} onclick={saveSleepNow}>{t('accounts.save')}</button>
			</div>
			<small class="help">{t('settings.sleep_help', { zone: s.timezone || s.zone_effective || 'UTC' })}</small>
			{#if sleepBad}<span class="feedback bad" role="alert">{t(sleepBad)}</span>{/if}
			{#if errs.sleep_times}<span class="feedback bad">{t('settings.save_failed')}</span>{/if}
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

		<h3 class="sub-h"><span class="tile sm" aria-hidden="true"><Icon name="globe" size={13} /></span>{t('settings.proxy')}</h3>
		<div class="panel">
			<div class="row">
				<div class="lbl">
					<b id="px-l">{t('settings.proxy_use')}</b>
					<small>{proxyNoURL ? t('settings.proxy_needs_url') : t('settings.proxy_sub')}</small>
					{#if errs.proxy_enabled}<small class="bad">{t('settings.save_failed')}</small>{/if}
				</div>
				{#if proxyNoURL}
					<span class="chip off">{t('settings.unavailable')}</span>
				{:else}
					<button
						type="button"
						class="sw"
						role="switch"
						aria-checked={s.proxy.enabled}
						aria-labelledby="px-l"
						onclick={() => save('proxy_enabled', { proxy_enabled: !s.proxy.enabled })}
					></button>
				{/if}
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
							bind:this={proxyInput}
							oninput={editProxy}
							onkeydown={(e) => {
								if (e.key === 'Enter' && e.isTrusted && proxyDirty) void saveProxyNow();
							}}
						/>
						<button type="button" class="btn" disabled={!proxyDirty} onclick={saveProxyNow}>{t('accounts.save')}</button>
						<button type="button" class="btn" disabled={proxyTesting || proxyURL.trim() === ''} onclick={testProxy}>
							{proxyTesting ? t('settings.proxy_testing') : t('settings.proxy_test')}
						</button>
					</div>
					<Hint text={t('settings.proxy_help')} examples={proxyExamples} onpick={pickScheme} />
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
	.times .btn {
		align-self: flex-end;
		height: 34px;
	}
	.times .field {
		flex: 1;
		min-width: 120px;
	}
</style>
