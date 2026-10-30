<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/app';
	import type { Account, ForceChannel, Platform, PointsSafety } from '$lib/api/types';
	import PageHead from '$lib/components/PageHead.svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { formatInt } from '$lib/format';
	import { t } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';
	import { toPut } from '$lib/force-watch';
	import { movePlatformChannels } from '$lib/points';
	import { reorder } from '$lib/drag';

	const live = getLive();
	type Tab = Platform | 'safety';
	let tab = $state<Tab>('twitch');
	let accountSettings = $state<Record<string, { enabled: boolean; only: boolean; streams: number }>>({});
	let accountBusy = $state<Record<string, boolean>>({});
	let accountError = $state<Record<string, boolean>>({});
	let safety = $state<PointsSafety>({ sleep_enabled: true, sleep_start: '02:00', sleep_end: '08:00', follow_raids: true });
	let safetyLoaded = $state(false);
	let safetyBusy = $state(false);
	let safetyError = $state(false);
	let listSaving = $state(false);
	let listError = $state(false);
	let drag = $state<{ from: number; to: number } | null>(null);

	$effect(() => {
		for (const a of live.accounts) {
			if (accountSettings[a.id]) continue;
			accountSettings[a.id] = { enabled: a.points_enabled ?? true, only: a.points_only ?? false, streams: a.points_streams ?? 2 };
		}
	});

	const channels = $derived((live.forceWatch ?? []).filter((c) => c.platform === tab));
	const rows = $derived(drag ? movePlatformChannels(live.forceWatch ?? [], tab as Platform, drag.from, drag.to).filter((c) => c.platform === tab) : channels);
	const accountNames = $derived(new Map(live.accounts.map((a) => [a.id, a.name])));

	onMount(() => {
		void api.pointsSafety().then((value) => {
			safety = value;
			safetyLoaded = true;
		}).catch(() => {
			safetyError = true;
			safetyLoaded = true;
		});
	});

	function accountValue(a: Account): { enabled: boolean; only: boolean; streams: number } {
		return accountSettings[a.id] ?? { enabled: a.points_enabled ?? true, only: a.points_only ?? false, streams: a.points_streams ?? 2 };
	}

	async function saveAccount(a: Account, patch: Partial<{ enabled: boolean; only: boolean; streams: number }>): Promise<void> {
		const previous = accountValue(a);
		const next = { ...previous, ...patch };
		if (a.platform === 'twitch' && (!Number.isInteger(next.streams) || next.streams < 1)) return;
		accountSettings[a.id] = next;
		accountError[a.id] = false;
		accountBusy[a.id] = true;
		try {
			await api.setAccountPoints(a.id, {
				points_enabled: next.enabled,
				points_only: next.only,
				...(a.platform === 'twitch' ? { points_streams: next.streams } : {})
			});
			const updated = { ...a, points_enabled: next.enabled, points_only: next.only, points_streams: a.platform === 'twitch' ? next.streams : a.points_streams };
			live.accounts = live.accounts.map((item) => item.id === a.id ? updated : item);
		} catch {
			accountSettings[a.id] = previous;
			accountError[a.id] = true;
		} finally {
			accountBusy[a.id] = false;
		}
	}

	async function saveSafety(): Promise<void> {
		if (safetyBusy || safety.sleep_start === safety.sleep_end) return;
		safetyBusy = true;
		safetyError = false;
		try {
			safety = await api.savePointsSafety(safety);
		} catch {
			safetyError = true;
		} finally {
			safetyBusy = false;
		}
	}

	async function commitChannels(next: ForceChannel[]): Promise<void> {
		if (listSaving) return;
		listSaving = true;
		listError = false;
		const previous = live.forceWatch;
		live.forceWatch = next;
		try {
			live.forceWatch = (await api.putForceWatch(toPut(next, new Set(live.accounts.map((a) => a.id))))).channels;
		} catch {
			live.forceWatch = previous;
			try {
				live.forceWatch = (await api.forceWatch()).channels;
			} catch {
				/* the force_watch event refetches */
			}
			listError = true;
		} finally {
			listSaving = false;
		}
	}

	function move(from: number, to: number): void {
		if (from === to || !live.forceWatch) return;
		void commitChannels(movePlatformChannels(live.forceWatch, tab as Platform, from, to));
	}

	const dragOpts = {
		items: (list: HTMLElement) => Array.from(list.querySelectorAll<HTMLElement>(':scope > li')),
		enabled: () => !listSaving,
		onStart: (from: number) => (drag = { from, to: from }),
		onMove: (to: number) => { if (drag) drag.to = to; },
		onEnd: (from: number, to: number) => { drag = null; move(from, to); },
		onCancel: () => (drag = null)
	};

	function accountBalances(channel: ForceChannel): string[] {
		return (channel.points ?? []).map((point) => {
			const name = accountNames.get(point.account_id) ?? point.account_id;
			return point.balance === null ? t('points.balance_waiting', { name }) : t('points.balance', { name, balance: formatInt(point.balance) });
		});
	}
</script>

<PageHead title={t('nav.points')}>
	<div class="tabs" role="tablist" aria-label={t('points.tabs')}>
		{#each [{ id: 'twitch', label: 'accounts.twitch' }, { id: 'kick', label: 'accounts.kick' }, { id: 'safety', label: 'points.safety' }] as item (item.id)}
			<button type="button" role="tab" aria-selected={tab === item.id} class:on={tab === item.id} onclick={() => (tab = item.id as Tab)}>{t(item.label)}</button>
		{/each}
	</div>

	{#if tab === 'safety'}
		<section class="panel" aria-labelledby="safety-h">
			<h2 id="safety-h">{t('points.safety')}</h2>
			{#if !safetyLoaded}<p>{t('points.loading')}</p>{:else}
				<label class="check"><input type="checkbox" bind:checked={safety.sleep_enabled} />{t('points.sleep_enabled')}</label>
				<div class="times">
					<label>{t('points.sleep_start')}<input type="time" bind:value={safety.sleep_start} disabled={!safety.sleep_enabled} /></label>
					<label>{t('points.sleep_end')}<input type="time" bind:value={safety.sleep_end} disabled={!safety.sleep_enabled} /></label>
				</div>
				<label class="check"><input type="checkbox" bind:checked={safety.follow_raids} />{t('points.follow_raids')}</label>
				{#if safety.sleep_start === safety.sleep_end}<p class="bad" role="alert">{t('points.sleep_times_equal')}</p>{/if}
				{#if safetyError}<p class="bad" role="alert">{t('points.save_failed')}</p>{/if}
				<button type="button" class="btn" disabled={safetyBusy || safety.sleep_start === safety.sleep_end} onclick={saveSafety}>{safetyBusy ? t('points.saving') : t('points.save')}</button>
			{/if}
		</section>
	{:else}
		<section class="panel" aria-labelledby="accounts-h">
			<h2 id="accounts-h">{t('points.account_settings')}</h2>
			{#if live.accounts.filter((a) => a.platform === tab).length === 0}<p>{t('points.no_accounts')}</p>{/if}
			{#each live.accounts.filter((a) => a.platform === tab) as a (a.id)}
				{@const value = accountValue(a)}
				<div class="account" class:busy={accountBusy[a.id]}>
					<strong>{a.name}</strong>
					<label class="check"><input type="checkbox" checked={value.enabled} disabled={accountBusy[a.id]} onchange={(e) => void saveAccount(a, { enabled: e.currentTarget.checked })} />{t('points.enabled')}</label>
					<label class="check"><input type="checkbox" checked={value.only} disabled={accountBusy[a.id]} onchange={(e) => void saveAccount(a, { only: e.currentTarget.checked })} />{t('points.only')}</label>
					{#if a.platform === 'twitch'}
						<label class="stream">{t('points.streams')}<input type="number" min="1" step="1" value={value.streams} disabled={accountBusy[a.id]} onchange={(e) => void saveAccount(a, { streams: e.currentTarget.valueAsNumber })} /></label>
					{/if}
					{#if accountError[a.id]}<p class="bad" role="alert">{t('points.save_failed')}</p>{/if}
				</div>
			{/each}
		</section>
		<section class="panel" aria-labelledby="channels-h">
			<h2 id="channels-h">{t('points.channels', { platform: t(tab === 'twitch' ? 'accounts.twitch' : 'accounts.kick') })}</h2>
			<p class="hint">{t('points.channel_hint')}</p>
			{#if listError}<p class="bad" role="alert">{t('points.save_failed')}</p>{/if}
			{#if rows.length === 0}<p>{t('points.no_channels')}</p>{:else}
				<ol use:reorder={dragOpts}>
					{#each rows as channel, i (`${channel.platform}/${channel.channel}`)}
						<li class:dragging={drag && drag.to === i}>
							<div class="row"><span class="rank">{i + 1}</span><PlatformMark platform={channel.platform} /><strong>{channel.channel}</strong>
								<div class="tools" role="group" aria-label={channel.channel}>
									<button type="button" aria-label={t('points.move_up')} title={t('points.move_up')} disabled={i === 0 || listSaving} onclick={() => move(i, i - 1)}>↑</button>
									<button type="button" aria-label={t('points.move_down')} title={t('points.move_down')} disabled={i === rows.length - 1 || listSaving} onclick={() => move(i, i + 1)}>↓</button>
								</div>
							</div>
							{#if channel.platform === 'twitch' && (channel.points?.length ?? 0) > 0}<small class="balances">{accountBalances(channel).join(' · ')}</small>{/if}
						</li>
					{/each}
				</ol>
			{/if}
		</section>
	{/if}
</PageHead>

<style>
	.tabs{display:flex;gap:8px;border-bottom:1px solid var(--border);margin-bottom:18px}.tabs button{font:inherit;color:var(--muted);background:transparent;border:0;border-bottom:2px solid transparent;padding:10px 14px;cursor:pointer}.tabs button.on{color:var(--text);border-color:var(--accent)}
	.panel{background:var(--surface);border:1px solid var(--border);border-radius:12px;padding:18px;margin-bottom:16px}.panel h2{font-size:17px;margin:0 0 14px}.account{display:flex;align-items:center;gap:18px;flex-wrap:wrap;border-top:1px solid var(--border);padding:12px 0}.account:first-of-type{border-top:0}.account strong{min-width:120px}.check{display:flex;align-items:center;gap:8px}.check input{accent-color:var(--accent)}.stream,.times label{display:grid;gap:5px;color:var(--muted);font-size:13px}.stream input,.times input{font:inherit;color:var(--text);background:var(--bg);border:1px solid var(--border);border-radius:7px;padding:7px}.stream input{width:110px}.times{display:flex;gap:14px;margin:14px 0}.times input{width:145px}.hint{color:var(--muted);font-size:13px}ol{list-style:none;padding:0;margin:0}li{border-top:1px solid var(--border);padding:10px 0}.row{display:flex;align-items:center;gap:10px}.rank{width:24px;color:var(--muted)}.row strong{flex:1}.tools{display:flex;gap:5px}.tools button{width:34px;height:34px;border:1px solid var(--border);border-radius:7px;background:var(--bg);color:var(--text);cursor:pointer}.tools button:disabled{opacity:.45;cursor:default}.balances{display:block;margin:4px 0 0 34px;color:var(--muted)}.dragging{opacity:.5}.bad{color:var(--red)}.btn{margin-top:14px}.busy{opacity:.7}
	@media(max-width:600px){.account{gap:12px}.account strong{width:100%}.times{flex-direction:column}}
</style>
