<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon from '$lib/components/Icon.svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import type { Platform } from '$lib/api/types';
	import type { PointsState } from './_state.svelte';
	import Add from './_Add.svelte';

	let { st, children }: { st: PointsState; children: Snippet } = $props();

	const TABS: { id: Platform; label: string }[] = [
		{ id: 'twitch', label: 'Twitch' },
		{ id: 'kick', label: 'Kick' }
	];

	const watchers = $derived(
		st.lists[st.tab].flatMap((c) => (c.live ? c.watching.map((id) => `${st.name(id)} on ${c.name}`) : []))
	);
</script>

<div class="pp">
	<div class="head">
		<div class="head-l">
			<h1>Points</h1>
			{#if watchers.length}
				<span class="now"><span class="dot"></span>Force watching · {watchers.join(', ')}</span>
			{:else}
				<span class="now grey"><span class="dot"></span>Nobody on Force watch</span>
			{/if}
		</div>
		{#if st.tab === 'twitch'}
			<button type="button" class="btn" disabled={st.refreshing} onclick={() => st.refresh()}>
				<Icon name="refresh" />{st.refreshing ? 'Fetching…' : 'Refresh balances'}
			</button>
		{/if}
	</div>

	<div class="tabs" role="tablist" aria-label="Platform">
		{#each TABS as tb (tb.id)}
			<button
				type="button"
				role="tab"
				aria-selected={st.tab === tb.id}
				onclick={() => {
					st.tab = tb.id;
					st.drag = null;
				}}
			>
				<PlatformMark platform={tb.id} />{tb.label}<span class="n">{st.lists[tb.id].length}</span>
			</button>
		{/each}
	</div>

	<p class="hint">
		{#if st.tab === 'twitch'}
			Force watch runs for an account only when it has no drop to mine. <b>Points only</b> accounts never mine drops and
			always watch this list. Top channel that is live wins.
		{:else}
			Force watch runs for an account only when it has no drop to mine. Top channel that is live wins. Kick has no
			channel points, so balances are unavailable.
		{/if}
	</p>

	{@render children()}

	<Add {st} />
</div>

<style>
	.pp {
		color: var(--text);
		min-width: 0;
	}
	.head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		margin-bottom: 16px;
		flex-wrap: wrap;
	}
	.head-l {
		display: flex;
		align-items: center;
		gap: 8px 16px;
		flex-wrap: wrap;
		min-width: 0;
	}
	h1 {
		font-size: 18px;
		font-weight: 600;
		margin: 0;
		letter-spacing: -0.01em;
	}
	.now {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		font-size: 13px;
		font-weight: 500;
		color: var(--green);
		min-width: 0;
	}
	.now .dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: currentColor;
		flex: none;
	}
	.now.grey {
		color: var(--t2);
	}
	.now.grey .dot {
		background: var(--t3);
	}
	.tabs {
		display: flex;
		gap: 4px;
		border-bottom: 1px solid var(--border-soft);
		margin-bottom: 12px;
	}
	.tabs button {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		font: inherit;
		font-size: 13.5px;
		font-weight: 500;
		padding: 8px 12px;
		margin-bottom: -1px;
		border: 0;
		border-bottom: 2px solid transparent;
		background: none;
		color: var(--t2);
		cursor: pointer;
	}
	.tabs button:hover {
		color: var(--text);
	}
	.tabs button[aria-selected='true'] {
		color: var(--text);
		border-bottom-color: var(--accent);
	}
	.tabs button:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
		border-radius: 6px;
	}
	.n {
		font: 500 11px/1 var(--mono);
		padding: 3px 6px;
		border-radius: 999px;
		background: var(--surface-2);
		color: var(--t3);
	}
	.hint {
		margin: 0 0 16px;
		font-size: 12.5px;
		color: var(--t3);
		max-width: 80ch;
	}
	.hint b {
		color: var(--t2);
		font-weight: 600;
	}

	/* Shared bits for the variant partials. */
	.pp :global(.btn) {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font: inherit;
		font-size: 12.5px;
		font-weight: 500;
		height: 30px;
		padding: 0 12px;
		border-radius: 6px;
		border: 1px solid var(--border);
		background: var(--surface-2);
		color: var(--text);
		cursor: pointer;
		white-space: nowrap;
	}
	.pp :global(.btn:hover:not(:disabled)) {
		border-color: var(--accent);
	}
	.pp :global(.btn.primary) {
		background: var(--accent-strong);
		border-color: var(--accent-strong);
		color: #fff;
	}
	.pp :global(.btn:disabled) {
		opacity: 0.5;
		cursor: default;
	}
	.pp :global(button:focus-visible),
	.pp :global(input:focus-visible) {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.pp :global(.rh) {
		margin: 0 0 8px;
		font-size: 11px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--t3);
	}
	.pp :global(.mono) {
		font-family: var(--mono);
		font-variant-numeric: tabular-nums;
	}
	.pp :global(.paused) {
		opacity: 0.55;
	}
	.pp :global(.tag) {
		font-size: 10.5px;
		font-weight: 600;
		padding: 1px 6px;
		border-radius: 999px;
		background: var(--surface-3);
		color: var(--t2);
		white-space: nowrap;
	}
	.pp :global(.fresh-stale) {
		color: var(--amber);
	}
	.pp :global(.fresh-failed) {
		color: var(--red);
	}
	.pp :global(.rmv) {
		width: 26px;
		height: 26px;
		display: grid;
		place-items: center;
		flex: none;
		border: 0;
		border-radius: 6px;
		background: none;
		color: var(--t3);
		cursor: pointer;
	}
	.pp :global(.rmv:hover) {
		background: var(--red-soft);
		color: var(--red);
	}
	.pp :global(.moved) {
		border-color: var(--accent) !important;
	}
	.pp :global(.dragging) {
		border-color: var(--accent) !important;
		background: var(--surface-2) !important;
		box-shadow: 0 8px 24px rgb(0 0 0 / 0.35);
		position: relative;
		z-index: 1;
	}
	@media (max-width: 520px) {
		.head .btn {
			width: 100%;
			justify-content: center;
		}
	}
</style>
