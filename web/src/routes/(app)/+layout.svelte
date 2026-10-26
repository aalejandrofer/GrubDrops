<script lang="ts">
	import { goto } from '$app/navigation';
	import { onDestroy, setContext, untrack } from 'svelte';
	import { api, session } from '$lib/app';
	import { Unreachable } from '$lib/api/client';
	import type { UpdateInfo } from '$lib/api/types';
	import TopBar from '$lib/components/TopBar.svelte';
	import { LIVE_KEY } from '$lib/live-context';
	import { createLive, type EventSourceLike } from '$lib/stores/live.svelte';

	let { children } = $props();

	const live = createLive({
		api,
		eventSource: (url) => new EventSource(url) as unknown as EventSourceLike,
		onUnauthorized: () => void goto('/login')
	});
	setContext(LIVE_KEY, live);

	let phase = $state<'boot' | 'offline' | 'ready'>('boot');
	let update = $state<UpdateInfo | null>(null);
	let started = false;

	function startLive(): void {
		if (started) return;
		started = true;
		live.start();
	}

	async function boot(): Promise<void> {
		try {
			await session.load();
		} catch (e) {
			if (!(e instanceof Unreachable)) throw e;
			// Offline: the live store retries and reports it; boot again once it reconnects.
			phase = 'offline';
			startLive();
			return;
		}
		if (session.setupNeeded) return void goto('/setup');
		if (!session.authed) return void goto('/login');
		phase = 'ready';
		startLive();
		api
			.update()
			.then((u) => (update = u))
			.catch(() => {});
	}

	void boot();

	$effect(() => {
		if (phase === 'offline' && live.updatedAt !== null && !live.unreachable) untrack(() => void boot());
	});

	onDestroy(() => live.stop());
</script>

{#if phase !== 'boot'}
	<TopBar version={update?.current || session.version} {update} />
	<main>
		{@render children()}
	</main>
{/if}

<style>
	main {
		max-width: 1160px;
		margin: 0 auto;
		padding: 28px 24px 96px;
	}
	@media (max-width: 720px) {
		main {
			padding: 20px 16px 80px;
		}
	}
</style>
