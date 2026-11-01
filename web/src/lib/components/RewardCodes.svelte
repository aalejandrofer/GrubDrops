<script lang="ts">
	import { api } from '$lib/app';
	import type { RewardCode, RewardCodeState } from '$lib/api/types';
	import CopyCommand from '$lib/components/CopyCommand.svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { t } from '$lib/i18n/index.svelte';
	import { expiresIn, liveCodes, redeemHref } from '$lib/reward-codes';

	let codes = $state<RewardCode[]>([]);
	let busy = $state<number | null>(null);
	let failed = $state(false);
	let now = $state(Date.now());

	async function load(): Promise<void> {
		try {
			codes = (await api.rewardCodes()).codes;
		} catch {
			codes = [];
		}
	}

	$effect(() => {
		void load();
		const tick = setInterval(() => (now = Date.now()), 60_000);
		const reload = setInterval(() => void load(), 15 * 60_000);
		return () => {
			clearInterval(tick);
			clearInterval(reload);
		};
	});

	const shown = $derived(liveCodes(codes, now));

	async function mark(c: RewardCode, state: RewardCodeState): Promise<void> {
		busy = c.id;
		failed = false;
		try {
			await api.setRewardCode(c.id, state);
			codes = codes.filter((x) => x.id !== c.id);
		} catch {
			failed = true;
		} finally {
			busy = null;
		}
	}
</script>

{#if shown.length > 0}
	<section class="sec" aria-label={t('rewards.title')}>
		<div class="sec-h">
			<h2>
				<svg viewBox="0 0 16 16" fill="none" stroke="var(--accent)" stroke-width="1.6" aria-hidden="true"
					><circle cx="5.5" cy="8" r="2.8" /><path d="M8.3 8H14m-2 0v2.2M10 8v1.6" stroke-linecap="round" /></svg
				>{t('rewards.title')} <span class="n">{shown.length}</span>
			</h2>
			{#if failed}<small class="bad" role="alert">{t('settings.save_failed')}</small>{/if}
		</div>
		<div class="panel">
			{#each shown as c (c.id)}
				{@const left = expiresIn(c.expires_at, now)}
				{@const href = redeemHref(c.redeem_url)}
				<div class="row">
					<div class="what">
						<div class="t">{c.name}</div>
						<div class="c">
							{#if c.game}<span>{c.game}</span>{/if}
							<span class="acct"><PlatformMark platform="twitch" size="sm" />{c.account_name}</span>
							{#if left}<span class="exp">{t(left.key, { n: left.n })}</span>{/if}
						</div>
					</div>
					<div class="code"><CopyCommand command={c.code} /></div>
					<div class="acts">
						{#if href}<a class="btn go" {href} target="_blank" rel="noopener noreferrer">{t('rewards.redeem')}</a>{/if}
						<button type="button" class="btn" disabled={busy === c.id} onclick={() => mark(c, 'redeemed')}
							>{t('rewards.redeemed')}</button
						>
						<button type="button" class="btn" disabled={busy === c.id} onclick={() => mark(c, 'dismissed')}
							>{t('rewards.dismiss')}</button
						>
					</div>
				</div>
			{/each}
		</div>
	</section>
{/if}

<style>
	.sec {
		margin-top: 28px;
		min-width: 0;
	}
	.sec:first-child {
		margin-top: 0;
	}
	.sec-h {
		display: flex;
		align-items: center;
		gap: 8px;
		margin-bottom: 8px;
		min-height: 26px;
		flex-wrap: wrap;
	}
	.sec-h h2 {
		margin: 0;
		font-size: 13px;
		font-weight: 600;
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.sec-h h2 svg {
		width: 14px;
		height: 14px;
		flex: none;
	}
	.n {
		font: 400 12px var(--mono);
		color: var(--t2);
		background: var(--surface-3);
		border-radius: 10px;
		padding: 0 6px;
	}
	.bad {
		font-size: 12px;
		color: var(--red);
	}
	.panel {
		background: var(--surface);
		border: 1px solid var(--border-soft);
		border-radius: var(--radius);
		overflow: hidden;
	}
	.panel > .row + .row {
		border-top: 1px solid var(--border-soft);
	}
	.row {
		display: grid;
		grid-template-columns: minmax(0, 1fr) minmax(0, 240px) auto;
		gap: 16px;
		align-items: center;
		padding: 12px 14px;
	}
	.t {
		font-size: 13px;
		font-weight: 500;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.c {
		font-size: 12px;
		color: var(--t2);
		display: flex;
		align-items: center;
		gap: 8px;
		margin-top: 3px;
		flex-wrap: wrap;
	}
	.acct {
		display: inline-flex;
		align-items: center;
		gap: 5px;
	}
	.exp {
		color: var(--amber);
	}
	.code {
		min-width: 0;
	}
	.acts {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		flex-wrap: wrap;
		justify-content: flex-end;
	}
	.btn {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font-size: 12px;
		font-weight: 500;
		background: var(--surface-2);
		border: 1px solid var(--border);
		border-radius: 6px;
		padding: 3px 9px;
		color: var(--text);
		white-space: nowrap;
		text-decoration: none;
		cursor: pointer;
	}
	.btn:hover:not([disabled]) {
		border-color: var(--t3);
		background: var(--surface-3);
	}
	.btn[disabled] {
		opacity: 0.45;
		cursor: default;
	}
	.btn:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.btn.go {
		border-color: var(--accent);
		color: var(--accent);
	}
	@media (max-width: 720px) {
		.row {
			grid-template-columns: minmax(0, 1fr);
			gap: 10px;
		}
		.acts {
			justify-content: flex-start;
		}
	}
</style>
