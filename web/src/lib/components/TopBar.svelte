<script lang="ts">
	import { page } from '$app/state';
	import logo from '../../../../assets/img/logo.png';
	import type { UpdateInfo } from '$lib/api/types';
	import { t } from '$lib/i18n/index.svelte';
	import VersionPopover from './VersionPopover.svelte';

	let { version, update }: { version: string; update: UpdateInfo | null } = $props();

	const items = [
		{ href: '/', key: 'nav.home' },
		{ href: '/discover', key: 'nav.discover' },
		{ href: '/games', key: 'nav.games' },
		{ href: '/points', key: 'nav.points' },
		{ href: '/inventory', key: 'nav.inventory' },
		{ href: '/settings', key: 'nav.settings' }
	];
	const on = (href: string) =>
		href === '/' ? page.url.pathname === '/' : page.url.pathname === href || page.url.pathname.startsWith(`${href}/`);

	// On narrow screens the links scroll sideways; keep the current one in view.
	let tabs: HTMLElement | undefined = $state();
	$effect(() => {
		void page.url.pathname;
		const nav = tabs;
		if (!nav) return;
		const reveal = () => {
			const a = nav.querySelector<HTMLElement>('a.on');
			if (!a) return;
			const t = nav.getBoundingClientRect();
			const r = a.getBoundingClientRect();
			if (r.right > t.right) nav.scrollLeft += r.right - t.right;
			else if (r.left < t.left) nav.scrollLeft -= t.left - r.left;
		};
		reveal();
		if (typeof ResizeObserver === 'undefined') return;
		const ro = new ResizeObserver(reveal);
		ro.observe(nav);
		return () => ro.disconnect();
	});
</script>

<header class="top">
	<div class="top-in">
		<a href="/" class="brand"><img src={logo} alt="" /><span>{t('app.name')}</span></a>
		<nav class="tabs" bind:this={tabs}>
			{#each items as it (it.href)}
				<a href={it.href} class:on={on(it.href)} aria-current={on(it.href) ? 'page' : undefined}>{t(it.key)}</a>
			{/each}
		</nav>
		<VersionPopover current={version} {update} />
	</div>
</header>

<style>
	.top {
		position: sticky;
		top: 0;
		z-index: 10;
		background: color-mix(in srgb, var(--bg) 92%, transparent);
		border-bottom: 1px solid var(--border-soft);
		backdrop-filter: blur(6px);
	}
	.top-in {
		max-width: 1160px;
		margin: 0 auto;
		height: 52px;
		padding: 0 24px;
		display: flex;
		align-items: center;
		gap: 28px;
	}
	a {
		color: inherit;
		text-decoration: none;
	}
	.brand {
		display: flex;
		align-items: center;
		gap: 8px;
		font-weight: 600;
		letter-spacing: -0.01em;
	}
	.brand img {
		width: 24px;
		height: 24px;
		border-radius: 6px;
	}
	.tabs {
		display: flex;
		gap: 2px;
	}
	.tabs a {
		padding: 6px 10px;
		border-radius: 6px;
		color: var(--t2);
		font-weight: 500;
		font-size: 13.5px;
	}
	.tabs a:hover {
		color: var(--text);
		background: var(--surface);
	}
	.tabs a.on {
		color: var(--text);
		background: var(--surface-2);
	}
	.tabs a:focus-visible,
	.brand:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	@media (max-width: 720px) {
		.top-in {
			padding: 0 16px;
			gap: 14px;
		}
		.brand span {
			display: none;
		}
		/* Links scroll inside the bar when they don't fit, so the page never scrolls sideways. */
		.tabs {
			min-width: 0;
			overflow-x: auto;
			scrollbar-width: none;
		}
		.tabs::-webkit-scrollbar {
			display: none;
		}
		.tabs a {
			padding: 6px 8px;
			white-space: nowrap;
		}
		.tabs a:focus-visible {
			outline-offset: -2px;
		}
	}
	@media (max-width: 420px) {
		.top-in {
			gap: 8px;
		}
		.tabs a {
			padding: 6px 5px;
			font-size: 13px;
		}
	}
</style>
