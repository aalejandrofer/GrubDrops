<script lang="ts">
	import { onDestroy } from 'svelte';
	import { t } from '$lib/i18n/index.svelte';

	/**
	 * Two-click hand mark. The first click arms the icon button into a text
	 * button; a second click within ARM_MS confirms. Blur, Escape or the
	 * timeout disarm it.
	 */
	let {
		kind,
		drop,
		account,
		disabled = false,
		onconfirm
	}: {
		kind: 'collect' | 'uncollect';
		drop: string;
		account: string;
		disabled?: boolean;
		onconfirm: () => void | Promise<void>;
	} = $props();

	const ARM_MS = 4000;
	let armed = $state(false);
	let pending = $state(false);
	let timer: ReturnType<typeof setTimeout> | undefined;
	onDestroy(() => clearTimeout(timer));

	const title = $derived(kind === 'collect' ? t('mark.collect') : t('mark.uncollect'));
	const label = $derived(
		kind === 'collect' ? t('mark.collect_label', { drop, account }) : t('mark.uncollect_label', { drop, account })
	);
	const confirm = $derived(kind === 'collect' ? t('mark.confirm_collect') : t('mark.confirm_uncollect'));

	function disarm(): void {
		clearTimeout(timer);
		armed = false;
	}

	async function click(): Promise<void> {
		if (disabled || pending) return;
		if (!armed) {
			armed = true;
			clearTimeout(timer);
			timer = setTimeout(disarm, ARM_MS);
			return;
		}
		disarm();
		pending = true;
		try {
			await onconfirm();
		} finally {
			pending = false;
		}
	}

	function onkeydown(e: KeyboardEvent): void {
		if (!armed || e.key !== 'Escape') return;
		// Escape disarms only; it must not also close a surrounding drawer.
		e.stopPropagation();
		disarm();
	}
</script>

<span class="arm">
	<button
		type="button"
		class="ab {kind}"
		class:armed
		aria-label={armed ? t('mark.armed_label', { action: confirm, drop, account }) : label}
		disabled={disabled || pending}
		onclick={click}
		onblur={disarm}
		{onkeydown}
	>
		{#if armed}
			{confirm}
		{:else if kind === 'collect'}
			<svg viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"
				><path d="m2.8 6.2 2.2 2.2 4.2-4.6" /></svg
			>
		{:else}
			<svg viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" aria-hidden="true"
				><path d="m3.5 3.5 5 5m0-5-5 5" /></svg
			>
		{/if}
	</button>
	{#if !armed && !pending}<span class="tip" aria-hidden="true">{title}</span>{/if}
</span>

<style>
	/* The slot stays icon sized; the armed text grows leftwards over its
	   neighbours, so no row reflows while armed. */
	.arm {
		position: relative;
		display: inline-grid;
		width: 22px;
		height: 22px;
		flex: none;
	}
	.ab {
		width: 22px;
		height: 22px;
		display: grid;
		place-items: center;
		padding: 0;
		background: none;
		border: 1px solid transparent;
		border-radius: 4px;
		color: var(--t3);
		cursor: pointer;
		font: inherit;
	}
	.ab svg {
		width: 12px;
		height: 12px;
	}
	.ab:hover:not(:disabled) {
		border-color: var(--border);
		color: var(--text);
		background: var(--surface-2);
	}
	.ab.collect {
		color: var(--green);
	}
	.ab.uncollect {
		color: var(--red);
	}
	.ab.collect svg,
	.ab.uncollect svg {
		width: 14px;
		height: 14px;
	}
	.ab.collect:hover:not(:disabled) {
		color: var(--green);
		border-color: var(--green);
		background: var(--green-soft);
	}
	.ab.uncollect:hover:not(:disabled) {
		color: var(--red);
		border-color: var(--red);
		background: var(--red-soft);
	}
	.tip {
		position: absolute;
		right: 0;
		bottom: calc(100% + 6px);
		z-index: 2;
		padding: 3px 8px;
		border: 1px solid var(--border);
		border-radius: 5px;
		background: var(--surface-2);
		color: var(--text);
		font-size: 12px;
		white-space: nowrap;
		pointer-events: none;
		opacity: 0;
		transition: opacity 0.1s;
	}
	.ab:hover:not(:disabled) + .tip,
	.ab:focus-visible + .tip {
		opacity: 1;
	}
	.ab:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.ab:disabled {
		opacity: 0.5;
		cursor: default;
	}
	.ab.armed {
		position: absolute;
		right: 0;
		top: 50%;
		transform: translateY(-50%);
		z-index: 1;
		width: auto;
		height: 24px;
		padding: 0 9px;
		white-space: nowrap;
		font-size: 12px;
		font-weight: 600;
	}
	.ab.armed.collect,
	.ab.armed.collect:hover:not(:disabled) {
		color: var(--green);
		border-color: var(--green);
		background: linear-gradient(var(--green-soft), var(--green-soft)), var(--surface);
	}
	.ab.armed.uncollect,
	.ab.armed.uncollect:hover:not(:disabled) {
		color: var(--red);
		border-color: var(--red);
		background: linear-gradient(var(--red-soft), var(--red-soft)), var(--surface);
	}
</style>
