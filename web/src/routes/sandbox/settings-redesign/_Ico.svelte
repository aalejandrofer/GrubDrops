<script lang="ts">
	import type { IconName } from './_data';

	/** Sandbox-only icon set: the app's Icon has eight glyphs, the mockups need more. */
	const P: Record<IconName, string[]> = {
		users: ['M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2', 'M13 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0', 'M22 21v-2a4 4 0 0 0-3-3.87', 'M16 3.13a4 4 0 0 1 0 7.75'],
		sort: ['M10 6h11', 'M10 12h11', 'M10 18h11', 'M4 6h1v4', 'M4 10h2', 'M6 18H4c0-1 2-2 2-3s-1-1.5-2-1'],
		moon: ['M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z'],
		globe: ['M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0z', 'M2 12h20', 'M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z'],
		flask: ['M10 2v7.3', 'M14 9.3V2', 'M8.5 2h7', 'M14 9.3a6.5 6.5 0 1 1-4 0', 'M5.5 16h13'],
		send: ['m22 2-7 20-4-9-9-4Z', 'M22 2 11 13'],
		bell: ['M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9', 'M10.3 21a1.94 1.94 0 0 0 3.4 0'],
		sliders: ['M4 21v-7', 'M4 10V3', 'M12 21v-9', 'M12 8V3', 'M20 21v-5', 'M20 12V3', 'M2 14h4', 'M10 8h4', 'M18 16h4'],
		pulse: ['M22 12h-4l-3 9L9 3l-3 9H2'],
		download: ['M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4', 'm7 10 5 5 5-5', 'M12 15V3'],
		lock: ['M5 11h14a2 2 0 0 1 2 2v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-7a2 2 0 0 1 2-2z', 'M7 11V7a5 5 0 0 1 10 0v4'],
		cpu: ['M6 4h12a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2z', 'M9 9h6v6H9z', 'M9 1v3M15 1v3M9 20v3M15 20v3M20 9h3M20 14h3M1 9h3M1 14h3'],
		search: ['m21 21-4.3-4.3', 'M19 11a8 8 0 1 1-16 0 8 8 0 0 1 16 0z'],
		'chevron-right': ['m9 18 6-6-6-6'],
		'chevron-left': ['m15 18-6-6 6-6'],
		'chevron-down': ['m6 9 6 6 6-6'],
		check: ['M20 6 9 17l-5-5'],
		x: ['M18 6 6 18M6 6l12 12'],
		alert: ['M12 9v4M12 17h.01M10.3 3.9 2 18a2 2 0 0 0 1.7 3h16.6a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z'],
		copy: ['M9 9h11v11H9z', 'M5 15H4V4h11v1'],
		play: ['m7 4 13 8-13 8z'],
		refresh: ['M21 12a9 9 0 1 1-2.6-6.4L21 8', 'M21 3v5h-5'],
		plus: ['M12 5v14M5 12h14'],
		'log-out': ['M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4', 'm16 17 5-5-5-5', 'M21 12H9'],
		grid: ['M3 3h7v7H3z', 'M14 3h7v7h-7z', 'M14 14h7v7h-7z', 'M3 14h7v7H3z'],
		chat: ['M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z']
	};

	let { name, size = 16 }: { name: IconName; size?: number } = $props();
</script>

<svg
	class="ico"
	width={size}
	height={size}
	viewBox="0 0 24 24"
	fill="none"
	stroke="currentColor"
	stroke-width="1.9"
	stroke-linecap="round"
	stroke-linejoin="round"
	aria-hidden="true"
>
	{#each P[name] as d, i (i)}<path {d} />{/each}
</svg>

<style>
	.ico {
		flex: none;
		display: block;
	}
</style>
