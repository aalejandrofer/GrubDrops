<script lang="ts">
	import NewPill from '$lib/components/NewPill.svelte';
	import type { Account } from '$lib/api/types';
	import { fieldKey, pointsTotal } from '$lib/account-panel';
	import { pointsSupported } from '$lib/accounts';
	import PointsMode from '$lib/components/PointsMode.svelte';
	import { formatInt } from '$lib/format';
	import { t } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';
	import { accountFresh, freshLine, freshTone } from '$lib/points';
	import type { AccountActions } from './account-actions.svelte';
	import SaveMark from './SaveMark.svelte';

	/** Channel points: total balance, points mode picker, and a link to the channels on Points. */
	let { act, a }: { act: AccountActions; a: Account } = $props();

	const live = getLive();
	let now = $state(Date.now());
	$effect(() => {
		const h = setInterval(() => (now = Date.now()), 30_000);
		return () => clearInterval(h);
	});

	const total = $derived(pointsTotal(live.points, a.id));
	const f = $derived(accountFresh(live.points, a.id));
	const fresh = $derived(freshLine(f, now));
	const err = $derived(act.note(a.id, 'points'));
	const PLATFORM = { twitch: 'Twitch', kick: 'Kick' } as const;
</script>

<section class="sec" aria-labelledby="pt-{a.id}">
	<h4 class="sh" id="pt-{a.id}">{t('accounts.points_h')}</h4>
	{#if pointsSupported(a)}
		<div class="pt">
			<div class="tot">
				<span class="n mono" class:off={a.points_mode === 'off'}>{total === null ? '–' : formatInt(total)}</span>
				<span class="u">{t('points.pts')}</span>
			</div>
			{#if fresh}<small class="fresh {freshTone(f)}">{fresh}</small>{/if}
		</div>
		<div class="mode">
			<b>{t('accounts.points_mode')}<NewPill since="2.3" /><SaveMark mark={act.marks[fieldKey(a.id, 'points')]} /></b>
			<PointsMode {a} onpick={(m) => act.setPointsMode(a, m)} />
			<small>{t('accounts.points_sub')}</small>
		</div>
		{#if err}<p class="note bad" role="alert">{err.text}</p>{/if}
	{:else}
		<p class="none">{t('points.unsupported', { platform: PLATFORM[a.platform] })}</p>
	{/if}
	<a class="lnk" href="/points"
		>{t('accounts.points_link')}<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12h14M13 6l6 6-6 6" /></svg></a
	>
</section>

<style>
	.pt {
		display: grid;
		gap: 2px;
	}
	.tot {
		display: flex;
		align-items: baseline;
		gap: 6px;
	}
	.n {
		font-size: 24px;
		font-weight: 600;
		letter-spacing: -0.02em;
	}
	.n.off {
		color: var(--t2);
	}
	.u {
		color: var(--t2);
		font-size: 13px;
	}
	.fresh {
		color: var(--t3);
		font-size: 12.5px;
	}
	.fresh.stale {
		color: var(--amber);
	}
	.fresh.failed {
		color: var(--red);
	}
	.mode {
		display: grid;
		gap: 6px;
		min-width: 0;
	}
	.mode b {
		font-weight: 500;
		font-size: 13.5px;
	}
	.mode small {
		color: var(--t3);
		font-size: 12px;
	}
	.none {
		margin: 0;
		font-size: 13px;
		color: var(--t2);
	}
</style>
