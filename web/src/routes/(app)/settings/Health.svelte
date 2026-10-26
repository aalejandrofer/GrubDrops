<script lang="ts">
	import { t } from '$lib/i18n/index.svelte';
</script>

<section class="set" id="health" aria-labelledby="h-hea">
	<div class="sec-head"><div><h2 id="h-hea">{t('settings.health')}</h2></div></div>
	<p class="stub">{t('settings.placeholder')}</p>
</section>

<style>
	.stub {
		margin: 0;
		color: var(--t2);
		font-size: 13px;
	}
</style>
