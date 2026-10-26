<script lang="ts">
	import { t } from '$lib/i18n/index.svelte';
</script>

<section class="set" id="accounts" aria-labelledby="h-acc">
	<div class="sec-head"><div><h2 id="h-acc">{t('settings.accounts')}</h2></div></div>
	<p class="stub">{t('settings.placeholder')}</p>
</section>

<style>
	.stub {
		margin: 0;
		color: var(--t2);
		font-size: 13px;
	}
</style>
