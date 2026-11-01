<script lang="ts">
	import Icon from '$lib/components/Icon.svelte';
	import { reorder } from '$lib/drag';
	import { freshText, freshness, fwNote, pts, statusText } from './_data';
	import type { PointsState } from './_state.svelte';
	import Chan from './_Chan.svelte';
	import Grip from './_Grip.svelte';
	import Mode from './_Mode.svelte';

	let { st }: { st: PointsState } = $props();
	const tw = $derived(st.tab === 'twitch');

	/** Spotlit account: its tiles stay bright in the list, the rest fade. */
	let focus = $state<string | null>(null);
	const fx = $derived(st.tabAccts.some((a) => a.id === focus) ? focus : null);
	const n = $derived(st.lists[st.tab].length);

	function onCount(id: string): number {
		return st.lists[st.tab].filter((c) => c.accounts.includes(id)).length;
	}
</script>

<section class="wallets" aria-label="Accounts">
	{#each st.tabAccts as a (a.id)}
		<div class="wal" class:paused={!a.enabled} class:lit={fx === a.id}>
			<div class="top">
				<b class="pf-name {a.platform}">{a.name}</b>
				{#if !a.enabled}<span class="tag">Paused</span>{/if}
				<button
					type="button"
					class="spot"
					aria-pressed={fx === a.id}
					title="Highlight {a.name} in the list"
					onclick={() => (focus = fx === a.id ? null : a.id)}>{fx === a.id ? 'Focused' : 'Focus'}</button
				>
			</div>
			<small class="st" class:on={a.doing.kind === 'watching'}>{statusText(a)}</small>
			{#if tw}
				<div class="tot">
					<span class="mono">{pts(st.total(a.id))}</span><span class="u">pts</span>
					<small class="fresh-{freshness(a)}">{freshText(a)}</small>
				</div>
				<Mode {st} {a} />
			{:else}
				<div class="tot na"><small>Points unavailable on Kick</small></div>
			{/if}
			<p class="fw">{fwNote(a)} · on {onCount(a.id)} of {n}</p>
		</div>
	{/each}
</section>

<div class="lh">
	<h2 class="rh">Force watch order</h2>
	{#if fx}
		<span class="fx"
			>Focused on <b>{st.name(fx)}</b>
			<button type="button" class="clr" onclick={() => (focus = null)}>Clear</button></span
		>
	{/if}
</div>
<ol use:reorder={st.dragOpts}>
	{#each st.rows as c, i (c.id)}
		<li
			class:moved={st.moved === c.id}
			class:dragging={st.drag?.id === c.id}
			class:out={fx !== null && !c.accounts.includes(fx)}
		>
			<Grip {st} {i} n={st.rows.length} />
			<div class="mid">
				<Chan {st} {c} rank={i + 1} />
				<div class="tiles" role="group" aria-label="Accounts on {c.name}" style:--n={st.tabAccts.length}>
					{#each st.tabAccts as a (a.id)}
						{@const on = c.accounts.includes(a.id)}
						{@const b = tw ? st.balance(c.id, a.id) : null}
						{@const now = c.live && c.watching.includes(a.id)}
						<button
							type="button"
							class="tile"
							class:on
							class:paused={!a.enabled}
							class:skip={on && (a.mode === 'off' || !a.enabled)}
							class:fade={fx !== null && fx !== a.id}
							aria-pressed={on}
							aria-label="{a.name} watches {c.name}"
							title="{a.name}: {fwNote(a)}"
							onclick={() => st.toggle(c.id, a.id)}
						>
							<span class="nm">{#if now}<span class="live" aria-label="watching now"></span>{/if}{a.name}</span>
							{#if tw}<span class="bal mono">{b === null ? '–' : pts(b)}</span>{/if}
						</button>
					{/each}
				</div>
			</div>
			<button type="button" class="rmv" aria-label="Remove {c.name}" title="Remove" onclick={() => st.remove(c.id)}
				><Icon name="x" /></button
			>
		</li>
	{:else}
		<li class="empty">No channels yet. Add one below.</li>
	{/each}
</ol>
<p class="legend">
	Tiles follow the account order above. Filled = that account watches the channel{#if tw}, number = its cached points
		there{/if}. Dashed = ticked but skips Force watch (points off or paused). Focus an account to see its channels.
</p>

<style>
	.wallets {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
		gap: 10px;
		margin-bottom: 20px;
	}
	.wal {
		display: grid;
		gap: 8px;
		padding: 12px 14px;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		align-content: start;
		min-width: 0;
		transition: border-color 0.15s;
	}
	.wal.lit {
		border-color: var(--accent);
	}
	.top {
		display: flex;
		align-items: center;
		gap: 8px;
		min-width: 0;
	}
	.top b {
		min-width: 0;
		font-size: 15px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.spot {
		margin-left: auto;
		font: inherit;
		font-size: 11.5px;
		height: 24px;
		padding: 0 10px;
		border: 1px solid var(--border);
		border-radius: 999px;
		background: none;
		color: var(--t2);
		cursor: pointer;
		flex: none;
	}
	.spot:hover {
		color: var(--text);
		border-color: var(--accent);
	}
	.spot[aria-pressed='true'] {
		border-color: var(--accent);
		background: var(--accent-soft);
		color: var(--accent);
		font-weight: 600;
	}
	.st {
		margin-top: -6px;
		font-size: 12.5px;
		color: var(--t3);
	}
	.st.on {
		color: var(--green);
		font-weight: 600;
	}
	.tot {
		display: flex;
		align-items: baseline;
		flex-wrap: wrap;
		gap: 2px 8px;
	}
	.tot .mono {
		font-size: 22px;
		font-weight: 600;
		letter-spacing: -0.01em;
	}
	.tot .u {
		font-size: 12px;
		color: var(--t3);
	}
	.tot small {
		font-size: 12px;
		color: var(--t3);
	}
	.tot.na small {
		font-size: 12.5px;
	}
	.fw {
		margin: 0;
		font-size: 12px;
		color: var(--dim);
	}
	.lh {
		display: flex;
		align-items: baseline;
		flex-wrap: wrap;
		gap: 4px 12px;
	}
	.fx {
		display: inline-flex;
		align-items: baseline;
		gap: 6px;
		font-size: 12px;
		color: var(--t3);
	}
	.fx b {
		color: var(--accent);
	}
	.clr {
		font: inherit;
		font-size: 12px;
		padding: 0;
		border: 0;
		background: none;
		color: var(--t2);
		text-decoration: underline;
		cursor: pointer;
	}
	ol {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 6px;
	}
	li {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 8px 8px 8px 4px;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		transition: border-color 0.15s;
	}
	li.out .mid > :global(.ch) {
		opacity: 0.5;
	}
	li.empty {
		padding: 14px;
		color: var(--dim);
		font-size: 13px;
	}
	.mid {
		flex: 1;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: 8px 16px;
		flex-wrap: wrap;
	}
	.mid > :global(.ch) {
		flex: 0 1 220px;
	}
	.tiles {
		flex: 1;
		min-width: 0;
		display: grid;
		grid-template-columns: repeat(var(--n), minmax(0, 120px));
		gap: 6px;
	}
	.tile {
		display: grid;
		gap: 1px;
		justify-items: start;
		min-width: 0;
		font: inherit;
		text-align: left;
		padding: 5px 9px;
		border-radius: 8px;
		border: 1px solid var(--border);
		background: var(--surface-2);
		color: var(--t3);
		cursor: pointer;
		transition: opacity 0.15s;
	}
	.tile.on {
		border-color: var(--accent);
		background: var(--accent-soft);
		color: var(--text);
	}
	.tile.skip {
		border-style: dashed;
	}
	.tile.fade {
		opacity: 0.3;
	}
	.tile.paused.fade {
		opacity: 0.2;
	}
	.nm {
		display: block;
		max-width: 100%;
		font-size: 11.5px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.bal {
		font-size: 12.5px;
		font-weight: 600;
		color: var(--t2);
	}
	.tile.on .bal {
		color: var(--text);
	}
	.tile:not(.on) .bal {
		color: var(--dim);
		font-weight: 500;
	}
	.live {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--green);
		box-shadow: 0 0 0 3px var(--green-soft);
		display: inline-block;
		margin-right: 6px;
		vertical-align: 1px;
	}
	.legend {
		margin: 8px 0 0;
		font-size: 12px;
		color: var(--dim);
	}
	@media (max-width: 640px) {
		.mid > :global(.ch) {
			flex-basis: 100%;
		}
		.tiles {
			grid-template-columns: repeat(var(--n), minmax(0, 1fr));
		}
	}
</style>
