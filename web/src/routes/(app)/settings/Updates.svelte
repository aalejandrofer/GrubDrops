<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/app';
	import type { Settings, SettingsPatch, UpdateInfo } from '$lib/api/types';
	import CopyCommand from '$lib/components/CopyCommand.svelte';
	import { httpsOnly, relTime } from '$lib/format';
	import { t } from '$lib/i18n/index.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import { SECTION_LOOK, updateChip } from '$lib/settings-look';
	import SectionHead from './SectionHead.svelte';

	let {
		s,
		save,
		errs
	}: {
		s: Settings;
		save: (key: string, p: SettingsPatch) => Promise<boolean>;
		errs: Record<string, boolean>;
	} = $props();

	let u = $state<UpdateInfo | null>(null);
	let checking = $state(false);
	let failed = $state(false);
	let now = $state(Date.now());

	onMount(() => {
		api
			.update()
			.then((r) => (u = r))
			.catch(() => {});
		const id = setInterval(() => (now = Date.now()), 30000);
		return () => clearInterval(id);
	});

	async function check(): Promise<void> {
		checking = true;
		failed = false;
		try {
			u = await api.checkUpdate();
			now = Date.now();
		} catch {
			failed = true;
		} finally {
			checking = false;
		}
	}

	const latestLabel = $derived(
		u?.checked_at
			? t('settings.latest') + ' · ' + t('settings.checked', { rel: relTime(u.checked_at, now) })
			: t('settings.latest')
	);
	const hasNew = $derived(u?.update_available === true);
	const notesUrl = $derived(httpsOnly(u?.notes_url));
	const chip = $derived(updateChip(u));
</script>

<section class="set hue-{SECTION_LOOK.updates.hue}" id="updates" aria-labelledby="h-upd">
	<SectionHead sec="updates" id="h-upd" blurb={t('settings.updates_sub')}>
		{#snippet chips()}
			{#if chip}<span class="chip {chip.tone}">{chip.text}</span>{/if}
		{/snippet}
		{#snippet right()}
			<button type="button" class="btn tint" disabled={checking} onclick={check}>
				<Icon name="refresh" />
				<span>{checking ? t('settings.checking') : t('settings.check')}</span>
			</button>
		{/snippet}
	</SectionHead>
	{#if failed}<p class="bad" role="status">{t('settings.check_failed')}</p>{/if}
	<div class="panel">
		<div class="vers">
			<div>
				<small>{t('settings.current')}</small><b>{u?.current || '—'}</b>
			</div>
			<div class:new={hasNew}>
				<small>{latestLabel}</small>
				<b>{u?.latest || '—'}</b>
			</div>
		</div>
		{#if u && ((hasNew && u.notes.length > 0) || u.command)}
			<div class="card-body">
				{#if hasNew && u.notes.length > 0}
					<ul class="notes">
						{#each u.notes as n, i (i)}<li>{n}</li>{/each}
					</ul>
				{/if}
				{#if notesUrl}
					<a class="more" href={notesUrl} target="_blank" rel="noopener noreferrer">{t('version.full_notes')}</a>
				{/if}
				{#if u.command}<CopyCommand command={u.command} />{/if}
			</div>
		{/if}
		<div class="row">
			<div class="lbl">
				<b id="ua-l">{t('settings.check_auto')}</b><small>{t('settings.check_hourly')}</small>
				{#if errs.update_auto}<small class="bad">{t('settings.save_failed')}</small>{/if}
			</div>
			<button
				type="button"
				class="sw"
				role="switch"
				aria-checked={s.update_auto}
				aria-labelledby="ua-l"
				onclick={() => save('update_auto', { update_auto: !s.update_auto })}
			></button>
		</div>
	</div>
</section>

<style>
	.vers {
		display: grid;
		grid-template-columns: repeat(2, 1fr);
	}
	.vers > div {
		padding: 14px 16px;
		display: grid;
		gap: 2px;
		min-width: 0;
	}
	.vers > div + div {
		border-left: 1px solid var(--border-soft);
	}
	.vers small {
		color: var(--t2);
		font-size: 12px;
	}
	.vers b {
		font: 600 16px var(--mono);
		overflow-wrap: anywhere;
	}
	.vers .new b {
		color: var(--blue);
	}
	.notes {
		margin: 0;
		padding: 0 0 0 16px;
		font-size: 13px;
		display: grid;
		gap: 3px;
	}
	.notes li::marker {
		color: var(--t3);
	}
	.more {
		font-size: 12.5px;
		color: var(--blue);
	}
	.more:hover {
		text-decoration: underline;
	}
	.bad {
		color: var(--red);
		font-size: 12px;
		margin: 0 0 8px;
	}
</style>
