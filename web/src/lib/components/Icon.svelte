<script lang="ts">
	const ICONS = {
		alert: ['M12 9v4M12 17h.01M10.3 3.9 2 18a2 2 0 0 0 1.7 3h16.6a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z'],
		terminal: ['m4 17 6-5-6-5M12 19h8'],
		list: ['M4 6h16M4 12h10M4 18h6'],
		x: ['M18 6 6 18M6 6l12 12'],
		'chevron-down': ['m6 9 6 6 6-6'],
		'chevron-up': ['m18 15-6-6-6 6'],
		refresh: ['M21 12a9 9 0 1 1-2.6-6.4L21 8', 'M21 3v5h-5'],
		'check-circle': ['M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0z', 'm8.5 12.3 2.4 2.4 4.6-4.9'],
		check: ['M20 6 9 17l-5-5'],
		copy: ['M9 9h11v11H9z', 'M5 15H4V4h11v1'],
		sliders: ['M4 21v-7', 'M4 10V3', 'M12 21v-9', 'M12 8V3', 'M20 21v-5', 'M20 12V3', 'M2 14h4', 'M10 8h4', 'M18 16h4'],
		users: ['M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2', 'M13 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0', 'M22 21v-2a4 4 0 0 0-3-3.87', 'M16 3.13a4 4 0 0 1 0 7.75'],
		cpu: [
			'M6 4h12a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2z',
			'M9 9h6v6H9z',
			'M9 1v3M15 1v3M9 20v3M15 20v3M20 9h3M20 14h3M1 9h3M1 14h3'
		],
		flask: ['M10 2v7.3', 'M14 9.3V2', 'M8.5 2h7', 'M14 9.3a6.5 6.5 0 1 1-4 0', 'M5.5 16h13'],
		bell: ['M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9', 'M10.3 21a1.94 1.94 0 0 0 3.4 0'],
		pulse: ['M22 12h-4l-3 9L9 3l-3 9H2'],
		download: ['M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4', 'm7 10 5 5 5-5', 'M12 15V3'],
		lock: ['M5 11h14a2 2 0 0 1 2 2v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-7a2 2 0 0 1 2-2z', 'M7 11V7a5 5 0 0 1 10 0v4'],
		moon: ['M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z'],
		globe: [
			'M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0z',
			'M2 12h20',
			'M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z'
		],
		plus: ['M12 5v14M5 12h14'],
		chat: ['M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z'],
		send: ['m22 2-7 20-4-9-9-4Z', 'M22 2 11 13']
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
