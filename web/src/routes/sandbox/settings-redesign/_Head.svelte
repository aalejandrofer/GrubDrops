<script lang="ts">
	import type { Snippet } from 'svelte';
	import Ico from './_Ico.svelte';
	import type { Hue, IconName } from './_data';

	let {
		label,
		icon,
		hue,
		blurb = '',
		id,
		chip,
		right
	}: { label: string; icon: IconName; hue: Hue; blurb?: string; id?: string; chip?: Snippet; right?: Snippet } = $props();
</script>

<div class="head hue-{hue}">
	<span class="tile"><Ico name={icon} /></span>
	<div class="t">
		<h2 {id}>{label}{#if chip}<span class="c">{@render chip()}</span>{/if}</h2>
		{#if blurb}<p>{blurb}</p>{/if}
	</div>
	{#if right}<div class="r">{@render right()}</div>{/if}
</div>

<style>
	.head {
		display: flex;
		align-items: center;
		gap: 12px;
		margin-bottom: 14px;
		flex-wrap: wrap;
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
	.c {
		display: inline-flex;
		gap: 6px;
	}
	p {
		margin: 1px 0 0;
		color: var(--t2);
		font-size: 13px;
	}
	.r {
		display: flex;
		gap: 8px;
		align-items: center;
	}
</style>
