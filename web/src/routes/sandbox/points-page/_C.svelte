<script lang="ts">
	import Icon from '$lib/components/Icon.svelte';
	import { reorder } from '$lib/drag';
	import { freshText, freshness, fwNote, pts, statusText, type Acct, type Chan as ChanT } from './_data';
	import type { PointsState } from './_state.svelte';
	import Chan from './_Chan.svelte';
	import Grip from './_Grip.svelte';
	import Mode from './_Mode.svelte';

	let { st }: { st: PointsState } = $props();
	const tw = $derived(st.tab === 'twitch');
	const accts = $derived(st.tabAccts);
	const tbodyRows = (el: HTMLElement) => Array.from(el.querySelectorAll<HTMLElement>(':scope > tr'));
</script>

{#snippet check(c: ChanT, a: Acct)}
	{@const on = c.accounts.includes(a.id)}
	{@const b = tw ? st.balance(c.id, a.id) : null}
	{@const now = c.live && c.watching.includes(a.id)}
	<button
		type="button"
		class="cell"
		class:on
		class:paused={!a.enabled}
		aria-pressed={on}
		aria-label="{a.name} watches {c.name}"
		title="{a.name}: {fwNote(a)}"
		onclick={() => st.toggle(c.id, a.id)}
	>
		<span class="box">{#if on}<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m5 12.5 4.5 4.5L19 7.5" /></svg>{/if}</span>
		{#if tw}<span class="bal mono">{b === null ? '–' : pts(b)}</span>{/if}
		{#if now}<span class="now">watching</span>{/if}
	</button>
{/snippet}

<!-- Wide: the grid. -->
<div class="wide">
	<table>
		<thead>
			<tr>
				<th class="chcol" scope="col">Channel</th>
				{#each accts as a (a.id)}
					<th scope="col">
						<div class="ah" class:paused={!a.enabled}>
							<span class="id"
								><b class="pf-name {a.platform}">{a.name}</b>{#if !a.enabled}<span class="tag">Paused</span>{/if}</span
							>
							<small>{statusText(a)}</small>
							{#if tw}<Mode {st} {a} short />{:else}<small class="dim">No points mode</small>{/if}
						</div>
					</th>
				{/each}
				<th class="x" scope="col"><span class="sr">Remove</span></th>
			</tr>
		</thead>
		<tbody use:reorder={{ ...st.dragOpts, items: tbodyRows }}>
			{#each st.rows as c, i (c.id)}
				<tr class:moved={st.moved === c.id} class:dragging={st.drag?.id === c.id}>
					<th scope="row" class="chcol">
						<span class="chc"><Grip {st} {i} n={st.rows.length} row /><Chan {st} {c} rank={i + 1} /></span>
					</th>
					{#each accts as a (a.id)}
						<td>{@render check(c, a)}</td>
					{/each}
					<td class="x"
						><button type="button" class="rmv" aria-label="Remove {c.name}" title="Remove" onclick={() => st.remove(c.id)}
							><Icon name="x" /></button
						></td
					>
				</tr>
			{/each}
		</tbody>
		<tfoot>
			<tr>
				<th scope="row" class="chcol">{tw ? 'Total' : 'Balance'}</th>
				{#each accts as a (a.id)}
					<td>
						{#if tw}
							<div class:paused={!a.enabled}>
								<b class="mono">{pts(st.total(a.id))}</b>
								<small class="fresh-{freshness(a)}">{freshText(a)}</small>
							</div>
						{:else}
							<small class="dim">Unavailable</small>
						{/if}
					</td>
				{/each}
				<td class="x"></td>
			</tr>
		</tfoot>
	</table>
	{#if st.rows.length === 0}<p class="empty">No channels yet. Add one below.</p>{/if}
</div>

<!-- Phone: account strip, then one card per channel. -->
<div class="narrow">
	<div class="accs">
		{#each accts as a (a.id)}
			<div class="acc" class:paused={!a.enabled}>
				<div class="top">
					<b class="pf-name {a.platform}">{a.name}</b>{#if !a.enabled}<span class="tag">Paused</span>{/if}
					<span class="sp"></span>
					{#if tw}<b class="mono">{pts(st.total(a.id))}</b>{:else}<small class="dim">No points</small>{/if}
				</div>
				{#if tw}
					<Mode {st} {a} />
					<small>{statusText(a)} · <span class="fresh-{freshness(a)}">{freshText(a)}</span></small>
				{:else}
					<small class="dim">{statusText(a)}</small>
				{/if}
			</div>
		{/each}
	</div>
	<ol use:reorder={st.dragOpts}>
		{#each st.rows as c, i (c.id)}
			<li class="cc" class:moved={st.moved === c.id} class:dragging={st.drag?.id === c.id}>
				<div class="cch">
					<Grip {st} {i} n={st.rows.length} />
					<Chan {st} {c} rank={i + 1} />
					<button type="button" class="rmv" aria-label="Remove {c.name}" title="Remove" onclick={() => st.remove(c.id)}
						><Icon name="x" /></button
					>
				</div>
				<div class="ccb">
					{#each accts as a (a.id)}
						<div class="ar">
							<span class="pf-name {a.platform}" class:paused={!a.enabled}>{a.name}</span>
							{@render check(c, a)}
						</div>
					{/each}
				</div>
			</li>
		{:else}
			<li class="empty">No channels yet. Add one below.</li>
		{/each}
	</ol>
</div>

<style>
	.wide {
		overflow-x: auto;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
	}
	table {
		width: 100%;
		border-collapse: collapse;
		font-size: 13px;
	}
	th,
	td {
		padding: 8px 10px;
		text-align: left;
		vertical-align: middle;
		border-bottom: 1px solid var(--border-soft);
	}
	thead th {
		vertical-align: bottom;
		font-weight: 400;
		background: var(--surface-2);
	}
	tbody tr {
		background: var(--surface);
		transition: background 0.15s;
	}
	tbody tr:hover {
		background: var(--surface-2);
	}
	tbody tr.moved th {
		box-shadow: inset 3px 0 0 var(--accent);
	}
	tfoot td,
	tfoot th {
		border-bottom: 0;
		background: var(--surface-2);
		vertical-align: top;
	}
	tfoot th {
		font-size: 11px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--t3);
	}
	tfoot b {
		display: block;
		font-size: 15px;
	}
	tfoot small {
		font-size: 11.5px;
		color: var(--t3);
	}
	.chcol {
		min-width: 240px;
		font-weight: 400;
	}
	thead .chcol {
		font-size: 11px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--t3);
	}
	.chc {
		display: flex;
		align-items: center;
		gap: 6px;
	}
	.ah {
		display: grid;
		gap: 4px;
		min-width: 170px;
	}
	.id {
		display: flex;
		align-items: center;
		gap: 6px;
	}
	.ah small {
		font-size: 12px;
		color: var(--t3);
	}
	.dim {
		color: var(--dim) !important;
	}
	.x {
		width: 36px;
		padding-left: 0;
	}
	.sr {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		clip: rect(0 0 0 0);
	}
	.empty {
		margin: 0;
		padding: 14px;
		color: var(--dim);
		font-size: 13px;
	}
	.cell {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		font: inherit;
		padding: 4px 8px 4px 4px;
		border: 1px solid transparent;
		border-radius: 6px;
		background: none;
		color: var(--dim);
		cursor: pointer;
	}
	.cell:hover {
		border-color: var(--border);
	}
	.cell.on {
		color: var(--text);
	}
	.box {
		width: 16px;
		height: 16px;
		border-radius: 4px;
		border: 1.5px solid var(--border);
		display: grid;
		place-items: center;
		flex: none;
	}
	.cell.on .box {
		background: var(--accent-strong);
		border-color: var(--accent-strong);
	}
	.box svg {
		width: 12px;
		height: 12px;
		fill: none;
		stroke: #fff;
		stroke-width: 3;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
	.bal {
		font-size: 12.5px;
	}
	.now {
		font-size: 11px;
		font-weight: 600;
		color: var(--green);
	}
	.narrow {
		display: none;
	}
	@media (max-width: 720px) {
		.wide {
			display: none;
		}
		.narrow {
			display: grid;
			gap: 16px;
		}
	}
	.accs {
		display: grid;
		gap: 8px;
	}
	.acc {
		display: grid;
		gap: 8px;
		padding: 10px 12px;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
	}
	.acc small {
		font-size: 12px;
		color: var(--t3);
	}
	.top {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.top .mono {
		font-size: 15px;
	}
	.sp {
		flex: 1;
	}
	ol {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 8px;
	}
	.cc {
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		transition: border-color 0.15s;
	}
	.cch {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 8px 8px 8px 4px;
	}
	.cch > :global(.ch) {
		flex: 1;
	}
	.ccb {
		border-top: 1px solid var(--border-soft);
		padding: 4px 8px;
	}
	.ar {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		min-height: 36px;
		font-size: 13px;
		font-weight: 500;
	}
	.ar + .ar {
		border-top: 1px solid var(--border-soft);
	}
	li.empty {
		padding: 14px;
		color: var(--dim);
		border: 1px solid var(--border-soft);
		border-radius: 10px;
	}
</style>
