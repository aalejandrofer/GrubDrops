<script lang="ts">
	import type { UpdateInfo } from '$lib/api/types';
	import { httpsOnly } from '$lib/format';
	import { t } from '$lib/i18n/index.svelte';
	import CopyCommand from './CopyCommand.svelte';
	import Icon from './Icon.svelte';

	let { current, update }: { current: string; update: UpdateInfo | null } = $props();
	let open = $state(false);
	let trigger: HTMLButtonElement | undefined = $state();
	let pop: HTMLDivElement | undefined = $state();

	const hasUpdate = $derived(update?.update_available === true);
	const notesUrl = $derived(httpsOnly(update?.notes_url));
	const command = $derived(update?.command || 'docker compose pull && docker compose up -d');

	function close(refocus: boolean): void {
		open = false;
		if (refocus) trigger?.focus();
	}

	function onWindowPointer(e: PointerEvent): void {
		if (!open) return;
		const n = e.target as Node;
		if (pop?.contains(n) || trigger?.contains(n)) return;
		close(false);
	}

	function onWindowKey(e: KeyboardEvent): void {
		if (open && e.key === 'Escape') close(true);
	}
</script>

<svelte:window onpointerdown={onWindowPointer} onkeydown={onWindowKey} />

<span class="verwrap">
	<button
		type="button"
		class="ver"
		class:update={hasUpdate}
		aria-expanded={open}
		aria-controls="verpop"
		bind:this={trigger}
		onclick={() => (open = !open)}
		>{#if update && hasUpdate}<span class="vfrom">{current || 'dev'} →{' '}</span>{update.latest}<span class="vtail"
				>{' '}{t('version.badge_available')}</span
			>{:else}{current || 'dev'}{/if}</button
	>
	<div class="pop" id="verpop" role="dialog" aria-label={t('version.label')} hidden={!open} bind:this={pop}>
		{#if update && hasUpdate}
			<h3>{t('version.available', { v: update.latest })}</h3>
			{#if update.notes.length > 0}
				<ul>
					{#each update.notes as n, i (i)}<li>{n}</li>{/each}
				</ul>
			{/if}
			<div class="cmdrow"><CopyCommand {command} /></div>
			{#if notesUrl}<a class="more" href={notesUrl} target="_blank" rel="noopener noreferrer"
					>{t('version.full_notes')}</a
				>{/if}
		{:else}
			<h3>
				<span class="ok"><Icon name="check-circle" /></span>{t('version.latest', { v: current || 'dev' })}
			</h3>
			{#if notesUrl}<a class="more" href={notesUrl} target="_blank" rel="noopener noreferrer"
					>{t('version.notes')}</a
				>{/if}
		{/if}
	</div>
</span>

<style>
	@media (max-width: 720px) {
		.ver .vfrom,
		.ver .vtail {
			display: none;
		}
	}
	.verwrap {
		margin-left: auto;
		position: relative;
	}
	.ver {
		font: 12px var(--mono);
		color: var(--text);
		white-space: nowrap;
		background: none;
		border: 0;
		padding: 4px 6px;
		border-radius: 6px;
		cursor: pointer;
	}
	.ver:hover,
	.ver[aria-expanded='true'] {
		background: var(--surface);
	}
	.ver:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	.ver.update {
		color: var(--blue);
	}
	.pop {
		position: absolute;
		right: 0;
		top: calc(100% + 8px);
		width: 300px;
		max-width: calc(100vw - 32px);
		background: var(--surface-2);
		border: 1px solid var(--border);
		border-radius: 10px;
		box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
		padding: 14px;
		font-size: 13px;
		z-index: 20;
	}
	.pop[hidden] {
		display: none;
	}
	h3 {
		margin: 0;
		font-size: 13px;
		font-weight: 600;
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.ok {
		color: var(--green);
		display: inline-flex;
	}
	ul {
		margin: 10px 0 0;
		padding: 0 0 0 22px;
		display: grid;
		gap: 3px;
	}
	li::marker {
		color: var(--t2);
	}
	.cmdrow {
		margin-top: 12px;
	}
	.more {
		display: inline-block;
		margin-top: 12px;
		color: var(--accent);
		font-size: 12px;
		text-decoration: none;
	}
	.more:hover {
		text-decoration: underline;
	}
</style>
