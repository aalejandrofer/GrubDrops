<script lang="ts">
	import { MODE_LABEL, MODE_SUB, pts, type Acct } from './_data';

	/** Channel points: read-only here, set on the Points page. */
	let { a, bare = false }: { a: Acct; bare?: boolean } = $props();
</script>

<section class="sec" aria-label="Channel points">
	{#if !bare}<h4 class="sh">Channel points</h4>{/if}
	{#if a.points}
		<div class="pt">
			<div class="tot">
				<span class="n mono" class:off={a.points.mode === 'off'}>{pts(a.points.total)}</span>
				<span class="u">points</span>
			</div>
			<small>Across {a.points.channels} Force watch channels · updated {a.points.fetched}</small>
		</div>
		<div class="opt">
			<div class="lbl">
				<b>Mode</b>
				<small>{MODE_SUB[a.points.mode]}</small>
			</div>
			<span class="mode" class:off={a.points.mode === 'off'}>{MODE_LABEL[a.points.mode]}</span>
		</div>
		<a class="lnk" href="/points"
			>Change mode and channels on Points<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12h14M13 6l6 6-6 6" /></svg
			></a
		>
	{:else}
		<p class="kick">
			Kick has no channel points. This account runs Force watch whenever it has no drop to mine.
		</p>
		<a class="lnk" href="/points"
			>Force watch channels on Points<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12h14M13 6l6 6-6 6" /></svg
			></a
		>
	{/if}
</section>

<style>
	.pt {
		display: grid;
		gap: 2px;
	}
	.tot {
		display: flex;
		align-items: baseline;
		gap: 6px;
	}
	.n {
		font-size: 24px;
		font-weight: 600;
		letter-spacing: -0.02em;
	}
	.n.off {
		color: var(--t2);
	}
	.u {
		color: var(--t2);
		font-size: 13px;
	}
	.pt small {
		color: var(--t3);
		font-size: 12.5px;
	}
	.mode {
		font-size: 12.5px;
		font-weight: 600;
		padding: 3px 10px;
		border-radius: 999px;
		background: var(--accent-soft);
		color: var(--accent);
		white-space: nowrap;
	}
	.mode.off {
		background: var(--surface-3);
		color: var(--t2);
	}
	.kick {
		margin: 0;
		font-size: 13px;
		color: var(--t2);
	}
</style>
