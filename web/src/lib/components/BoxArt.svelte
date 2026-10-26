<script lang="ts">
	let { src, name, size }: { src: string | null; name: string; size: 'lg' | 'sm' } = $props();
	let failed = $state(false);
	const initial = $derived(Array.from(name.trim())[0]?.toUpperCase() ?? '?');
	$effect(() => {
		src;
		failed = false;
	});
</script>

<div class="art {size}">
	{#if src && !failed}
		<img {src} alt="" loading="lazy" onerror={() => (failed = true)} />
	{:else}
		<span class="fallback" aria-hidden="true">{initial}</span>
	{/if}
</div>

<style>
	.art {
		position: relative;
		overflow: hidden;
		width: 100%;
		height: 100%;
		background: #22262e;
	}
	.lg {
		min-height: 150px;
	}
	.sm {
		min-height: 46px;
	}
	img {
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
		object-fit: cover;
		object-position: center 30%;
	}
	.fallback {
		position: absolute;
		inset: 0;
		display: grid;
		place-items: center;
		background: repeating-linear-gradient(135deg, var(--surface-3) 0 8px, var(--surface-2) 8px 16px);
		color: var(--t2);
		font-weight: 600;
	}
	.lg .fallback {
		font-size: 36px;
	}
	.sm .fallback {
		font-size: 16px;
	}
</style>
