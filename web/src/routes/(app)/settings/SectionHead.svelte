<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon from '$lib/components/Icon.svelte';
	import { t } from '$lib/i18n/index.svelte';
	import type { Section } from '$lib/settings';
	import { SECTION_LOOK } from '$lib/settings-look';

	/** Section heading: hue icon tile, title, state chips, one-line blurb and optional actions on the right. */
	let {
		sec,
		id,
		blurb = '',
		chips,
		right
	}: { sec: Section; id: string; blurb?: string; chips?: Snippet; right?: Snippet } = $props();

	const look = $derived(SECTION_LOOK[sec]);
</script>

<div class="sec-head">
	<span class="tile" aria-hidden="true"><Icon name={look.icon} size={16} /></span>
	<div class="t">
		<h2 {id}>{t(look.label)}{#if chips}<span class="chips">{@render chips()}</span>{/if}</h2>
		{#if blurb}<p>{blurb}</p>{/if}
	</div>
	{#if right}<div class="r">{@render right()}</div>{/if}
</div>

<style>
	.sec-head {
		display: flex;
		align-items: center;
		gap: 12px;
		flex-wrap: wrap;
		margin-bottom: 14px;
	}
	.t {
		flex: 1;
		min-width: 0;
	}
	h2 {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 4px 10px;
		margin: 0;
		font-size: 17px;
		font-weight: 600;
		letter-spacing: -0.015em;
	}
	.chips {
		display: inline-flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	p {
		margin: 1px 0 0;
		color: var(--t2);
		font-size: 13px;
	}
	.r {
		display: flex;
		align-items: center;
		gap: 8px;
	}
</style>
