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
</script>

<section class="strip" aria-label="Accounts">
	{#each st.tabAccts as a (a.id)}
		<div class="acc" class:paused={!a.enabled}>
			<div class="top">
				<b class="pf-name {a.platform}">{a.name}</b>
				{#if !a.enabled}<span class="tag">Paused</span>{/if}
				<small>{statusText(a)}</small>
			</div>
			{#if tw}
				<Mode {st} {a} short />
				<div class="tot">
					<span class="mono">{pts(st.total(a.id))}</span> pts
					<small class="fresh-{freshness(a)}">{freshText(a)}</small>
				</div>
			{:else}
				<div class="tot"><small>Points unavailable on Kick</small></div>
			{/if}
		</div>
	{/each}
</section>

<h2 class="rh">Force watch order</h2>
<ol use:reorder={st.dragOpts}>
	{#each st.rows as c, i (c.id)}
		<li class:moved={st.moved === c.id} class:dragging={st.drag?.id === c.id}>
			<Grip {st} {i} n={st.rows.length} />
			<div class="mid">
				<Chan {st} {c} rank={i + 1} />
				<div class="chips" role="group" aria-label="Accounts on {c.name}">
					{#each st.tabAccts as a (a.id)}
						{@const on = c.accounts.includes(a.id)}
						{@const b = tw ? st.balance(c.id, a.id) : null}
						{@const now = c.live && c.watching.includes(a.id)}
						<button
							type="button"
							class="chip"
							class:on
							class:paused={!a.enabled}
							class:skip={on && (a.mode === 'off' || !a.enabled)}
							aria-pressed={on}
							title="{a.name}: {fwNote(a)}"
							onclick={() => st.toggle(c.id, a.id)}
						>
							{#if now}<span class="live" aria-label="watching now"></span>{/if}
							<span class="nm">{a.name}</span>
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
{#if tw}
	<p class="legend">
		Chip on = that account watches the channel. Number = its cached points there. Dashed = ticked but skips Force watch
		(points off or paused).
	</p>
{/if}

<style>
	.strip {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(230px, 1fr));
		gap: 8px;
		margin-bottom: 20px;
	}
	.acc {
		display: grid;
		gap: 8px;
		padding: 10px 12px;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		align-content: start;
	}
	.top {
		display: flex;
		align-items: baseline;
		flex-wrap: wrap;
		gap: 4px 8px;
		min-width: 0;
	}
	.top b {
		font-size: 13.5px;
	}
	.top small {
		color: var(--t3);
		font-size: 12px;
	}
	.tot {
		display: flex;
		align-items: baseline;
		flex-wrap: wrap;
		gap: 4px 8px;
		font-size: 12px;
		color: var(--t3);
	}
	.tot .mono {
		font-size: 15px;
		font-weight: 600;
		color: var(--text);
	}
	.tot small {
		font-size: 11.5px;
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
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		flex: 1;
		min-width: 0;
	}
	.chip {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font: inherit;
		font-size: 12px;
		height: 26px;
		padding: 0 10px;
		border-radius: 999px;
		border: 1px solid var(--border);
		background: var(--surface-2);
		color: var(--t3);
		cursor: pointer;
	}
	.chip.on {
		border-color: var(--accent);
		background: var(--accent-soft);
		color: var(--text);
	}
	.chip.skip {
		border-style: dashed;
	}
	.chip .bal {
		font-size: 11.5px;
		color: var(--t2);
	}
	.chip:not(.on) .bal {
		color: var(--dim);
	}
	.live {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--green);
		box-shadow: 0 0 0 3px var(--green-soft);
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
	}
</style>
