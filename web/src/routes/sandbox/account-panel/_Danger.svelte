<script lang="ts">
	import type { Acct } from './_data';
	import type { PanelState } from './_state.svelte';

	/** Danger: restart the runner, remove the account. */
	let { st, a, bare = false }: { st: PanelState; a: Acct; bare?: boolean } = $props();

	const n = $derived(st.note[`${a.id}:run`]);
	const confirming = $derived(st.confirming === a.id);
</script>

<section class="sec danger-zone" aria-label="Danger">
	{#if !bare}<h4 class="sh">Danger</h4>{/if}
	<div class="opt">
		<div class="lbl">
			<b>Restart runner</b>
			<small>{a.enabled ? 'Stops and starts this account. Use it when mining looks stuck.' : 'Resume mining first.'}</small>
			{#if n}<small class="note {n.tone}" role="status">{n.text}</small>{/if}
		</div>
		<button type="button" class="btn" disabled={!a.enabled || n?.tone === 'wait'} onclick={() => st.restart(a)}>
			<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M21 12a9 9 0 1 1-2.6-6.4L21 8M21 3v5h-5" /></svg>Restart
		</button>
	</div>
	<div class="opt" class:conf={confirming}>
		<div class="lbl">
			<b>Remove account</b>
			{#if confirming}
				<small class="red">Remove {a.name}? Its claim history stays. You can add it back later.</small>
			{:else}
				<small>Signs out and stops mining. Claim history stays.</small>
			{/if}
		</div>
		{#if confirming}
			<div class="acts">
				<button type="button" class="btn danger solid" onclick={() => st.remove(a)}>Remove</button>
				<button type="button" class="btn" onclick={() => (st.confirming = null)}>Cancel</button>
			</div>
		{:else}
			<button type="button" class="btn danger" onclick={() => (st.confirming = a.id)}>Remove…</button>
		{/if}
	</div>
</section>

<style>
	.conf {
		padding: 10px;
		margin: -4px -10px;
		border-radius: 8px;
		background: var(--red-soft);
	}
	.red {
		color: color-mix(in srgb, var(--red) 70%, var(--text)) !important;
	}
</style>
