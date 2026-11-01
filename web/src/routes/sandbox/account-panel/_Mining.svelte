<script lang="ts">
	import { status, type Acct } from './_data';
	import type { PanelState } from './_state.svelte';
	import Saved from './_Saved.svelte';

	/** Mining: on/off, own games, skip sleep, Mine now. */
	let { st, a, bare = false }: { st: PanelState; a: Acct; bare?: boolean } = $props();

	const s = $derived(status(a));
	const n = $derived(st.note[`${a.id}:mine`]);
	const why = $derived(
		a.needs_login ? 'Sign in first.' : !a.enabled ? 'Resume mining first.' : !a.next ? 'Nothing else queued.' : ''
	);
</script>

<section class="sec" aria-label="Mining">
	{#if !bare}<h4 class="sh">Mining</h4>{/if}
	<div class="opt">
		<div class="lbl">
			<b id="en-{a.id}">Mining <Saved {st} k="{a.id}:enabled" /></b>
			<small>{a.enabled ? 'On. Turn off to pause this account; it stays signed in.' : 'Paused. The session stays signed in.'}</small>
		</div>
		<button
			type="button"
			class="sw"
			role="switch"
			aria-checked={a.enabled}
			aria-labelledby="en-{a.id}"
			onclick={() => st.setEnabled(a, !a.enabled)}
		></button>
	</div>
	<div class="opt">
		<div class="lbl">
			<b id="og-{a.id}">Own game list <Saved {st} k="{a.id}:own" /></b>
			{#if a.own_games}
				<small>{a.own.length} {a.own.length === 1 ? 'game' : 'games'}, top first: {a.own.join(', ')}.</small>
				<a class="lnk" href="/games">Edit list on Games</a>
			{:else}
				<small>Off: mines the shared Games list.</small>
			{/if}
		</div>
		<button
			type="button"
			class="sw"
			role="switch"
			aria-checked={a.own_games}
			aria-labelledby="og-{a.id}"
			onclick={() => st.setOwn(a, !a.own_games)}
		></button>
	</div>
	<div class="opt">
		<div class="lbl">
			<b id="sk-{a.id}">Skip sleep window <Saved {st} k="{a.id}:skip" /></b>
			<small>Keeps mining through the nightly sleep window (01:00 to 07:45).</small>
		</div>
		<button
			type="button"
			class="sw"
			role="switch"
			aria-checked={a.skip_sleep}
			aria-labelledby="sk-{a.id}"
			onclick={() => st.setSkip(a, !a.skip_sleep)}
		></button>
	</div>
	<div class="opt">
		<div class="lbl">
			<b>Up next</b>
			{#if a.next}
				<small>{a.next.game} · {a.next.drop} · {a.next.m} / {a.next.req} min</small>
			{:else}
				<small>Nothing queued.</small>
			{/if}
			{#if why && a.next}<small class="dim">{why}</small>{/if}
			{#if n}<small class="note {n.tone}" role="status">{n.text}</small>{/if}
		</div>
		<button type="button" class="btn" disabled={!!why || s === 'signed_out'} onclick={() => st.mineNow(a)}>
			<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M7 5v14l11-7z" /></svg>Mine now
		</button>
	</div>
</section>

<style>
	.dim {
		color: var(--t3) !important;
	}
</style>
