<script lang="ts">
	import { tick, untrack, type Snippet } from 'svelte';

	/**
	 * Side panel on the right edge, full width under 720px. A click or tap
	 * anywhere outside it closes it, and so does Escape; focus moves into it
	 * on open and back on close. Clicks inside an open dialog, on an element
	 * marked data-panel-keep, or on anything matching keep (triggers that swap
	 * the content) don't count as outside.
	 */
	let {
		open,
		label,
		onclose,
		keep = '',
		width = 440,
		bg = 'var(--surface-2)',
		dim = 'pass',
		restore,
		children
	}: {
		open: boolean;
		label: string;
		onclose: () => void;
		/** CSS selector for elements outside the panel whose clicks keep it open. */
		keep?: string;
		/** Width in px above 720px. */
		width?: number;
		bg?: string;
		/** pass: a light scrim at every width that clicks pass through. narrow: a dimmed backdrop under 720px only. */
		dim?: 'pass' | 'narrow';
		/** Element to focus on close; defaults to the one focused at open. */
		restore?: () => HTMLElement | null | undefined;
		children: Snippet;
	} = $props();

	let sheet = $state<HTMLElement | null>(null);
	let opener: HTMLElement | null = null;
	let wasOpen = false;

	const FOCUSABLE = '[data-autofocus], button:not(:disabled), [href], input:not(:disabled), select, textarea, [tabindex]:not([tabindex="-1"])';

	$effect(() => {
		const now = open;
		untrack(() => {
			if (now && !wasOpen) {
				opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
				void tick().then(() => sheet?.querySelector<HTMLElement>(FOCUSABLE)?.focus());
			} else if (!now && wasOpen) {
				// A click elsewhere already moved focus there; only a lost focus goes back.
				const lost = !document.activeElement || document.activeElement === document.body;
				if (lost) (restore?.() ?? opener)?.focus();
				opener = null;
			}
			wasOpen = now;
		});
	});

	function kept(path: EventTarget[]): boolean {
		return path.some(
			(n) =>
				n instanceof Element &&
				(n === sheet || n.matches('dialog[open], [data-panel-keep]') || (keep !== '' && n.matches(keep)))
		);
	}

	function onpointer(e: PointerEvent): void {
		if (!open || !sheet) return;
		if (!kept(e.composedPath())) onclose();
	}

	function onkey(e: KeyboardEvent): void {
		if (!open || e.key !== 'Escape' || e.defaultPrevented) return;
		// Escape in a dialog opened from the panel closes that dialog only.
		if (e.target instanceof Element && e.target.closest('dialog[open]')) return;
		onclose();
	}
</script>

<svelte:window onkeydown={onkey} onpointerdown={onpointer} />

{#if open}
	<div class="scrim {dim}" aria-hidden="true"></div>
	<div class="sheet" role="dialog" aria-modal="true" aria-label={label} style:--sp-w="{width}px" style:--sp-bg={bg} bind:this={sheet}>
		{@render children()}
	</div>
{/if}

<style>
	.scrim {
		position: fixed;
		inset: 0;
		z-index: 30;
	}
	.scrim.pass {
		background: rgb(0 0 0 / 0.3);
		pointer-events: none;
	}
	.scrim.narrow {
		display: none;
	}
	.sheet {
		position: fixed;
		top: 0;
		right: 0;
		bottom: 0;
		z-index: 31;
		width: min(var(--sp-w), 100%);
		display: flex;
		flex-direction: column;
		overflow: auto;
		overscroll-behavior: contain;
		background: var(--sp-bg);
		border-left: 1px solid var(--border);
		color: var(--text);
		box-shadow: -12px 0 32px rgb(0 0 0 / 0.25);
	}
	@media (max-width: 720px) {
		.sheet {
			width: 100%;
			border-left: 0;
		}
		.scrim.narrow {
			display: block;
			background: rgb(0 0 0 / 0.5);
		}
	}
</style>
