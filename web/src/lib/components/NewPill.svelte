<script lang="ts">
	import { session } from '$lib/app';
	import { t } from '$lib/i18n/index.svelte';
	import { newIn } from '$lib/nav';

	/**
	 * since is the minor release that added the feature, e.g. "2.3"; the pill drops on the next one.
	 * sup raises it top right of the word, like an exponent, for tight spots such as the top bar.
	 */
	let { since, sup = false }: { since: string; sup?: boolean } = $props();
</script>

{#if newIn(session.version, since)}<span class="new" class:sup>{t('nav.new')}</span>{/if}

<style>
	.new {
		display: inline-block;
		margin-left: 6px;
		padding: 1px 6px;
		border-radius: 999px;
		font-size: 10.5px;
		font-weight: 650;
		line-height: 1.5;
		letter-spacing: 0.02em;
		vertical-align: 1px;
		color: var(--accent);
		background: var(--accent-soft);
	}
	/* Raised and tucked into the link's right padding, so neighbours keep their spacing. */
	.new.sup {
		position: relative;
		top: -0.6em;
		margin: 0 -12px 0 3px;
		padding: 1px 4px;
		font-size: 9px;
		line-height: 1.2;
		vertical-align: baseline;
	}
</style>
