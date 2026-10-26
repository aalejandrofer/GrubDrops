<script lang="ts">
	import PageHead from '$lib/components/PageHead.svelte';
	import { t } from '$lib/i18n/index.svelte';
</script>

<PageHead title={t('nav.home')}>
	<div class="layout">
		<div class="cards"></div>
		<aside></aside>
	</div>
</PageHead>

<style>
	.layout {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 320px;
		gap: 24px;
		align-items: start;
	}
	@media (max-width: 960px) {
		.layout {
			grid-template-columns: minmax(0, 1fr);
		}
	}
</style>
