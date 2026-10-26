<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onDestroy, setContext } from 'svelte';
	import { api, session } from '$lib/app';
	import type { UpdateInfo } from '$lib/api/types';
	import TopBar from '$lib/components/TopBar.svelte';
	import { LIVE_KEY, NET_KEY } from '$lib/live-context';
	import { retryUnreachable } from '$lib/retry';
	import { createLive, type EventSourceLike } from '$lib/stores/live.svelte';

	let { children } = $props();

	// The first-run wizard is full screen, without the top bar.
	const bare = $derived(page.route.id === '/(app)/welcome');

	const live = createLive({
		api,
		eventSource: (url) => new EventSource(url) as unknown as EventSourceLike,
		onUnauthorized: () => void goto('/login')
	});
	setContext(LIVE_KEY, live);

	const net = $state({ down: false, retryIn: 0 });
	setContext(NET_KEY, net);

	let phase = $state<'boot' | 'offline' | 'ready'>('boot');
	let update = $state<UpdateInfo | null>(null);
	let started = false;
	let stopRetry: (() => void) | undefined;

	function startLive(): void {
		if (started) return;
		started = true;
		live.start();
	}

	async function boot(): Promise<void> {
		// While /api/session is Unreachable, show the offline header and retry every 5 s.
		const r = retryUnreachable(() => session.load(), {
			onState: (down, retryIn) => {
				net.down = down;
				net.retryIn = retryIn;
				if (down) {
					phase = 'offline';
					startLive();
				}
			}
		});
		stopRetry = r.stop;
		if (!(await r.done)) return;
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

	onDestroy(() => {
		stopRetry?.();
		live.stop();
	});
</script>

{#if phase !== 'boot' && bare}
	{@render children()}
{:else if phase !== 'boot'}
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
