<script lang="ts">
	import type { Account } from '$lib/api/types';
	import { fieldKey, STATUS_KEYS } from '$lib/account-panel';
	import { accountLine, accountStatus, initials, NAME_MAX } from '$lib/accounts';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import SidePanel from '$lib/components/SidePanel.svelte';
	import { t } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';
	import type { AccountActions } from './account-actions.svelte';
	import AccountDanger from './AccountDanger.svelte';
	import AccountLogin from './AccountLogin.svelte';
	import AccountMining from './AccountMining.svelte';
	import AccountPoints from './AccountPoints.svelte';
	import SaveMark from './SaveMark.svelte';

	/**
	 * Detail drawer for one account: header with rename, then Login, Mining,
	 * Channel points and Danger. Clicking another row swaps the account.
	 */
	let { act, busy = $bindable(false) }: { act: AccountActions; busy?: boolean } = $props();

	const live = getLive();
	const a = $derived(live.accounts.find((x) => x.id === act.open));
	const r = $derived(a ? live.runners.find((x) => x.account_id === a.id) : undefined);
	const st = $derived(a ? accountStatus(a, r) : 'idle');
	const nameNote = $derived(a ? act.note(a.id, 'name') : undefined);
	const PLATFORM = { twitch: 'Twitch', kick: 'Kick' } as const;

	// The row's open button gets focus back on close.
	const restore = () =>
		act.last ? document.querySelector<HTMLElement>(`[data-acct="${CSS.escape(act.last)}"] .who`) : null;

	function focusSelect(node: HTMLInputElement): void {
		node.focus();
		node.select();
	}

	function onRenameKey(e: KeyboardEvent, acc: Account): void {
		if (e.key === 'Enter') void act.commitRename(acc);
		else if (e.key === 'Escape') {
			e.stopPropagation();
			act.cancelRename();
		}
	}
</script>

<SidePanel
	open={!!a}
	label={a ? t('accounts.details_for', { name: a.name }) : ''}
	onclose={() => act.close()}
	keep="[data-acct]"
	bg="var(--bg)"
	dim="narrow"
	{restore}
>
	{#if a}
		<div class="top">
			<span class="crumb">{t('accounts.crumb', { platform: PLATFORM[a.platform] })}</span>
			<button type="button" class="ico" aria-label={t('accounts.close_details')} title={t('accounts.close')} onclick={() => act.close()}>
				<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12" /></svg>
			</button>
		</div>
		{#key a.id}
			<div class="body">
				<div class="hd">
					<span class="av big">
						{#if a.avatar_url}<img src={a.avatar_url} alt={t('accounts.avatar', { name: a.name })} />{:else}{initials(a.name)}{/if}
						<span class="mk"><PlatformMark platform={a.platform} /></span>
					</span>
					<div class="who-hd">
						{#if act.renaming === a.id}
							<div class="rn">
								<input
									class="inp"
									aria-label={t('accounts.rename_label', { name: a.name })}
									placeholder={a.platform_name}
									maxlength={NAME_MAX}
									bind:value={act.renameValue}
									use:focusSelect
									onkeydown={(e) => onRenameKey(e, a)}
								/>
								<button type="button" class="btn primary" onclick={() => act.commitRename(a)}>{t('accounts.save')}</button>
								<button type="button" class="btn" onclick={() => act.cancelRename()}>{t('accounts.cancel')}</button>
							</div>
						{:else}
							<div class="nm">
								<h3>{a.name}</h3>
								<button
									type="button"
									class="ico sm"
									aria-label={t('accounts.rename_label', { name: a.name })}
									title={t('accounts.rename_label', { name: a.name })}
									onclick={() => act.startRename(a)}
								>
									<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M4 20h4L19 9l-4-4L4 16v4zM14 6l4 4" /></svg>
								</button>
								<SaveMark mark={act.marks[fieldKey(a.id, 'name')]} />
							</div>
						{/if}
						{#if nameNote}<p class="note bad" role="alert">{nameNote.text}</p>{/if}
						<div class="tags">
							<span class="tag"><PlatformMark platform={a.platform} size="sm" />{PLATFORM[a.platform]} · @{a.platform_name}</span>
							<span class="st {st}">{t(STATUS_KEYS[st])}</span>
						</div>
						<p class="ln" class:bad={st === 'signed_out'}>{accountLine(a, r)}</p>
					</div>
				</div>
				<AccountLogin {act} {a} bind:busy />
				<AccountMining {act} {a} {r} />
				<AccountPoints {act} {a} />
				<AccountDanger {act} {a} />
			</div>
		{/key}
		<p class="foot">{t('accounts.autosave')}</p>
	{/if}
</SidePanel>

<style>
	.top {
		position: sticky;
		top: 0;
		z-index: 1;
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		padding: 10px 12px 10px 20px;
		border-bottom: 1px solid var(--border-soft);
		background: var(--bg);
	}
	.crumb {
		font-size: 12px;
		color: var(--t3);
	}
	.body {
		flex: 1;
		padding: 20px;
		display: grid;
		align-content: start;
		gap: 24px;
		min-width: 0;
	}
	.body > :global(.sec + .sec) {
		padding-top: 20px;
		border-top: 1px solid var(--border-soft);
	}
	.foot {
		margin: 0;
		padding: 10px 20px;
		border-top: 1px solid var(--border-soft);
		font-size: 12px;
		color: var(--t3);
	}
	.hd {
		display: flex;
		gap: 14px;
		align-items: flex-start;
		min-width: 0;
	}
	.av.big {
		width: 44px;
		height: 44px;
		font-size: 15px;
	}
	.who-hd {
		flex: 1;
		min-width: 0;
	}
	.nm {
		display: flex;
		align-items: center;
		gap: 4px;
		min-width: 0;
	}
	h3 {
		margin: 0;
		font-size: 17px;
		font-weight: 600;
		letter-spacing: -0.01em;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.ico.sm {
		width: 26px;
		height: 26px;
		flex: none;
	}
	.rn {
		display: flex;
		gap: 6px;
		align-items: center;
		flex-wrap: wrap;
	}
	.rn .inp {
		flex: 1 1 160px;
		width: auto;
		font-family: var(--font);
		font-size: 13.5px;
	}
	.tags {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		margin-top: 6px;
	}
	.tag {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		max-width: 100%;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.ln {
		margin: 6px 0 0;
		font-size: 12.5px;
		color: var(--t2);
		overflow-wrap: anywhere;
	}
	.ln.bad {
		color: color-mix(in srgb, var(--red) 70%, var(--text));
	}
	@media (max-width: 720px) {
		.body {
			padding: 16px;
		}
		.top {
			padding-left: 16px;
		}
	}
</style>
