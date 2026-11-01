<script lang="ts" module>
	import type { Snippet } from 'svelte';
	import type { Platform } from '$lib/api/types';

	/**
	 * One example under a field. `platform` or `icon` tags it. `value` is what a
	 * click fills in (defaults to `label`). `href` makes it a link that opens in
	 * a new tab, labelled in plain text instead of code.
	 */
	export type HintExample = { platform?: Platform; icon?: Snippet; label: string; value?: string; href?: string };
</script>

<script lang="ts">
	import { t } from '$lib/i18n/index.svelte';
	import PlatformMark from './PlatformMark.svelte';

	/**
	 * Field hint: one short line, then optional examples tagged by platform.
	 * `inline` puts the examples on the hint's line and wraps on narrow screens.
	 * `onpick` turns the examples into buttons that fill the field; `href` examples are links either way.
	 * `children` follows the text, for a link or other inline extra.
	 */
	let {
		text,
		examples = [],
		inline = false,
		onpick,
		children
	}: {
		text?: string;
		examples?: HintExample[];
		inline?: boolean;
		onpick?: (value: string) => void;
		children?: Snippet;
	} = $props();
</script>

{#snippet tag(ex: HintExample)}
	{#if ex.platform}<PlatformMark platform={ex.platform} />{:else if ex.icon}<span class="ic" aria-hidden="true">{@render ex.icon()}</span>{/if}
{/snippet}

<div class="hint" class:inline>
	{#if text || children}
		<p class="txt">{text ?? ''}{#if text && children}{' '}{/if}{#if children}{@render children()}{/if}</p>
	{/if}
	{#if examples.length > 0}
		<ul class="egs">
			{#each examples as ex, i (`${i}-${ex.label}`)}
				<li>
					{#if ex.href}
						<a class="eg pick" href={ex.href} target="_blank" rel="noopener noreferrer" aria-label={t('hint.new_tab', { label: ex.label })}>
							{@render tag(ex)}<span class="lb">{ex.label}</span>
							<svg class="ext" viewBox="0 0 24 24" aria-hidden="true"><path d="M7 17 17 7M9 7h8v8" /></svg>
						</a>
					{:else if onpick}
						<button type="button" class="eg pick" aria-label={t('hint.use', { value: ex.value ?? ex.label })} onclick={() => onpick(ex.value ?? ex.label)}>
							{@render tag(ex)}<code>{ex.label}</code>
						</button>
					{:else}
						<span class="eg">{@render tag(ex)}<code>{ex.label}</code></span>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}
</div>

<style>
	.hint {
		margin: 6px 2px 0;
		display: grid;
		gap: 4px;
		font-size: 12px;
		line-height: 1.45;
		color: var(--t3);
		min-width: 0;
	}
	.hint.inline {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		column-gap: 12px;
		row-gap: 4px;
	}
	.txt {
		margin: 0;
		max-width: 60ch;
	}
	.egs {
		margin: 0;
		padding: 0;
		list-style: none;
		display: flex;
		flex-wrap: wrap;
		gap: 2px 6px;
		min-width: 0;
	}
	.egs li {
		min-width: 0;
		max-width: 100%;
	}
	.eg {
		display: inline-flex;
		align-items: flex-start;
		gap: 6px;
		max-width: 100%;
		color: var(--t2);
	}
	.eg :global(.pm) {
		margin-top: 2px;
	}
	code {
		font: 11.5px/18px var(--mono);
		overflow-wrap: break-word;
		min-width: 0;
	}
	/* Clickable examples pad out to a 24px target; the offset keeps their text on the hint's left edge. */
	.pick {
		position: relative;
		left: -6px;
		margin: 0;
		padding: 3px 6px;
		border: 1px solid transparent;
		border-radius: 6px;
		background: none;
		color: var(--t2);
		font: inherit;
		cursor: pointer;
		text-align: left;
		text-decoration: none;
		transition:
			background 0.12s,
			border-color 0.12s,
			color 0.12s;
	}
	.ic {
		width: 14px;
		height: 14px;
		margin-top: 2px;
		flex: none;
		display: inline-grid;
		place-items: center;
	}
	.ic :global(svg) {
		width: 14px;
		height: 14px;
	}
	.lb {
		font-size: 12px;
		line-height: 18px;
	}
	.ext {
		width: 12px;
		height: 12px;
		margin-top: 3px;
		flex: none;
		fill: none;
		stroke: currentColor;
		stroke-width: 2;
		stroke-linecap: round;
		stroke-linejoin: round;
		color: var(--t3);
	}
	.pick .lb,
	.pick code {
		text-decoration: underline dotted;
		text-decoration-color: var(--border);
		text-underline-offset: 3px;
	}
	.pick:hover {
		background: var(--surface-2);
		border-color: var(--border-soft);
		color: var(--text);
	}
	.pick:hover .lb,
	.pick:hover code {
		text-decoration-color: transparent;
	}
	.pick:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	@media (prefers-reduced-motion: reduce) {
		.pick {
			transition: none;
		}
	}
</style>
