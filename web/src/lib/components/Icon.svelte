<script lang="ts">
	const ICONS = {
		alert: ['M12 9v4M12 17h.01M10.3 3.9 2 18a2 2 0 0 0 1.7 3h16.6a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z'],
		terminal: ['m4 17 6-5-6-5M12 19h8'],
		list: ['M4 6h16M4 12h10M4 18h6'],
		x: ['M18 6 6 18M6 6l12 12'],
		'chevron-down': ['m6 9 6 6 6-6'],
		'chevron-up': ['m18 15-6-6-6 6'],
		refresh: ['M21 12a9 9 0 1 1-2.6-6.4L21 8', 'M21 3v5h-5'],
		'check-circle': ['M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0z', 'm8.5 12.3 2.4 2.4 4.6-4.9']
	} as const;

	let { name, size = 14 }: { name: keyof typeof ICONS; size?: number } = $props();
</script>

<svg
	class="i"
	width={size}
	height={size}
	viewBox="0 0 24 24"
	fill="none"
	stroke="currentColor"
	stroke-width="1.8"
	stroke-linecap="round"
	stroke-linejoin="round"
	aria-hidden="true"
>
	{#each ICONS[name] as d (d)}<path {d} />{/each}
</svg>

<style>
	.i {
		flex: none;
	}
</style>
