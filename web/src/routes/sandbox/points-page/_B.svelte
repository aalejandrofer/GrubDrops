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

<div class="cards">
	{#each st.tabAccts as a (a.id)}
		<section class="card" class:paused={!a.enabled} aria-label={a.name}>
			<header>
				<div class="id">
					<b class="pf-name {a.platform}">{a.name}</b>
					{#if !a.enabled}<span class="tag">Paused</span>{/if}
				</div>
				<small class="st" class:on={a.doing.kind === 'watching'}>{statusText(a)}</small>
			</header>
			{#if tw}
				<Mode {st} {a} />
				<p class="fw">{fwNote(a)}</p>
				<div class="total">
					<span class="mono">{pts(st.total(a.id))}</span><span class="u">pts</span>
					<small class="fresh-{freshness(a)}">{freshText(a)}</small>
				</div>
			{:else}
				<p class="fw">{fwNote(a)}</p>
				<div class="total na"><small>Points unavailable on Kick</small></div>
			{/if}
			<ul>
				{#each st.lists[st.tab] as c, i (c.id)}
					{@const on = c.accounts.includes(a.id)}
					{@const b = tw ? st.balance(c.id, a.id) : null}
					{@const now = c.live && c.watching.includes(a.id)}
					<li class:off={!on}>
						<span class="rk mono">{i + 1}</span>
						<span class="dot" class:live={c.live} role="img" aria-label={c.live ? 'Live' : 'Offline'}></span>
						<span class="nm" class:now>{c.name}{#if now}<small>&nbsp;· watching</small>{/if}</span>
						{#if tw}<span class="bal mono">{b === null ? '–' : pts(b)}</span>{/if}
						<button
							type="button"
							class="sw"
							role="switch"
							aria-checked={on}
							aria-label="{a.name} watches {c.name}"
							onclick={() => st.toggle(c.id, a.id)}><span></span></button
						>
					</li>
				{/each}
			</ul>
		</section>
	{/each}
</div>

<section class="order" aria-labelledby="pp-b-order">
	<h2 class="rh" id="pp-b-order">Channel order · shared by every account</h2>
	<ol use:reorder={st.dragOpts}>
		{#each st.rows as c, i (c.id)}
			{@const n = c.accounts.length}
			<li class:moved={st.moved === c.id} class:dragging={st.drag?.id === c.id}>
				<Grip {st} {i} n={st.rows.length} row />
				<Chan {st} {c} rank={i + 1} />
				<span class="cnt">{n ? `${n} account${n > 1 ? 's' : ''}` : 'No accounts'}</span>
				<button type="button" class="rmv" aria-label="Remove {c.name}" title="Remove" onclick={() => st.remove(c.id)}
					><Icon name="x" /></button
				>
			</li>
		{:else}
			<li class="empty">No channels yet. Add one below.</li>
		{/each}
	</ol>
</section>

<style>
	.cards {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
		gap: 12px;
		align-items: start;
	}
	.card {
		display: grid;
		gap: 10px;
		padding: 14px;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		min-width: 0;
	}
	header {
		display: grid;
		gap: 2px;
	}
	.id {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.id b {
		font-size: 15px;
	}
	.st {
		font-size: 12.5px;
		color: var(--t3);
	}
	.st.on {
		color: var(--green);
		font-weight: 600;
	}
	.fw {
		margin: -4px 0 0;
		font-size: 12px;
		color: var(--dim);
	}
	.total {
		display: flex;
		align-items: baseline;
		flex-wrap: wrap;
		gap: 2px 8px;
		padding: 10px 0;
		border-top: 1px solid var(--border-soft);
		border-bottom: 1px solid var(--border-soft);
	}
	.total .mono {
		font-size: 22px;
		font-weight: 600;
		letter-spacing: -0.01em;
	}
	.total .u {
		font-size: 12px;
		color: var(--t3);
	}
	.total small {
		font-size: 12px;
		color: var(--t3);
		flex-basis: 100%;
	}
	.total.na small {
		flex-basis: auto;
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
	}
	li {
		display: grid;
		grid-template-columns: 18px 8px minmax(0, 1fr) auto auto;
		align-items: center;
		gap: 8px;
		min-height: 32px;
		font-size: 13px;
	}
	.card li + li {
		border-top: 1px solid var(--border-soft);
	}
	li.off .nm,
	li.off .bal {
		color: var(--dim);
	}
	.rk {
		font-size: 11px;
		color: var(--dim);
	}
	.dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--surface-3);
	}
	.dot.live {
		background: var(--red);
	}
	.nm {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.nm.now {
		color: var(--green);
		font-weight: 600;
	}
	.nm small {
		font-weight: 500;
	}
	.bal {
		font-size: 12px;
		color: var(--t2);
	}
	.sw {
		width: 30px;
		height: 18px;
		padding: 2px;
		border: 1px solid var(--border);
		border-radius: 999px;
		background: var(--surface-3);
		cursor: pointer;
		display: flex;
	}
	.sw span {
		width: 12px;
		height: 12px;
		border-radius: 50%;
		background: var(--t3);
		transition: transform 0.15s;
	}
	.sw[aria-checked='true'] {
		background: var(--accent-strong);
		border-color: var(--accent-strong);
	}
	.sw[aria-checked='true'] span {
		background: #fff;
		transform: translateX(12px);
	}
	.order {
		margin-top: 24px;
	}
	ol {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 4px;
	}
	ol li {
		grid-template-columns: auto minmax(0, 1fr) auto auto;
		padding: 6px 8px 6px 4px;
		border: 1px solid var(--border-soft);
		border-radius: 8px;
		background: var(--surface);
		transition: border-color 0.15s;
	}
	ol li.empty {
		display: block;
		padding: 14px;
		color: var(--dim);
	}
	.cnt {
		font-size: 12px;
		color: var(--t3);
		white-space: nowrap;
	}
	@media (max-width: 480px) {
		.cnt {
			display: none;
		}
	}
</style>
