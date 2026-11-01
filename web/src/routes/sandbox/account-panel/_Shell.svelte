<script lang="ts">
	import type { Snippet } from 'svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { initials, line, status, STATUS_LABEL, type Acct } from './_data';
	import type { PanelState } from './_state.svelte';
	import Menu from './_Menu.svelte';

	/** Settings > Accounts as it would look with the detail view: slimmer rows, no badges. */
	let {
		st,
		inline = false,
		below,
		children
	}: { st: PanelState; inline?: boolean; below?: Snippet<[Acct]>; children?: Snippet } = $props();

	const openFor = (a: Acct) => (inline ? st.isOpen(a.id) : st.open === a.id);
	const show = (a: Acct) => (inline ? st.toggleRow(a.id) : st.openPanel(a.id));

	function onWindowClick(e: MouseEvent): void {
		const inside = e.composedPath().some((n) => n instanceof Element && n.classList.contains('menu-wrap'));
		if (st.menu && !inside) st.menu = null;
	}
</script>

<svelte:window
	onclick={onWindowClick}
	onkeydown={(e) => {
		if (e.key !== 'Escape') return;
		if (st.menu) st.menu = null;
		else if (st.open && !inline) st.close();
	}}
/>

<div class="ap">
	<div class="sec-head">
		<div>
			<h2>Accounts</h2>
			<p>Every account here mines your chosen games. Open an account for its settings.</p>
		</div>
		<button type="button" class="btn">
			<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>Add account
		</button>
	</div>

	<div class="panel list">
		{#each st.accts as a (a.id)}
			{@const s = status(a)}
			{@const on = openFor(a)}
			{@const n = st.rowNote(a.id)}
			<div class="row acct" class:problem={s === 'signed_out'} class:on>
				<span class="av">
					{initials(a.name)}
					<span class="mk"><PlatformMark platform={a.platform} /></span>
				</span>
				<button
					type="button"
					class="who"
					aria-expanded={inline ? on : undefined}
					aria-label="Open {a.name} ({a.platform})"
					onclick={() => show(a)}
				>
					<b>{a.name}</b>
					<small>{line(a)}</small>
				</button>
				{#if n && !on}<small class="note {n.tone}" role="status">{n.text}</small>{/if}
				<span class="st {s}">{STATUS_LABEL[s]}</span>
				{#if s === 'signed_out'}
					<button
						type="button"
						class="btn primary fix"
						onclick={() => {
							show(a);
							st.relogin(a);
						}}>Log in again</button
					>
				{:else if s === 'paused'}
					<button type="button" class="btn fix" onclick={() => st.setEnabled(a, true)}>Resume mining</button>
				{/if}
				<Menu {st} {a} onopen={() => show(a)} />
				<button
					type="button"
					class="ico chev"
					class:down={inline && on}
					aria-label="{on ? 'Close' : 'Open'} {a.name}"
					onclick={() => show(a)}
				>
					<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="m9 6 6 6-6 6" /></svg>
				</button>
			</div>
			{#if below && on}{@render below(a)}{/if}
		{/each}
	</div>

	{@render children?.()}

	{#if st.toast}
		<div class="toast" role="status">
			<span>{st.toast.text}</span>
			{#if st.toast.undo}<button type="button" class="lnk" onclick={() => st.undoRemove()}>Undo</button>{/if}
		</div>
	{/if}
</div>

<style>
	.ap {
		color: var(--text);
		min-width: 0;
		font-size: 14px;
	}
	.sec-head {
		display: flex;
		align-items: flex-end;
		justify-content: space-between;
		gap: 12px;
		flex-wrap: wrap;
		margin-bottom: 14px;
	}
	.sec-head h2 {
		margin: 0;
		font-size: 15px;
		font-weight: 600;
		letter-spacing: -0.01em;
	}
	.sec-head p {
		margin: 2px 0 0;
		color: var(--t2);
		font-size: 13px;
	}
	.acct {
		cursor: default;
	}
	.acct.on {
		background: var(--surface-2);
		box-shadow: inset 2px 0 0 var(--accent);
	}
	.acct.problem {
		background: linear-gradient(90deg, color-mix(in srgb, var(--red) 7%, transparent), transparent 60%);
		box-shadow: inset 2px 0 0 var(--red);
	}
	.who {
		flex: 1;
		min-width: 0;
		border: 0;
		background: none;
		padding: 0;
		font: inherit;
		color: inherit;
		text-align: left;
		cursor: pointer;
	}
	.who b {
		display: block;
		font-weight: 500;
		font-size: 13.5px;
	}
	.who:hover b {
		text-decoration: underline;
		text-underline-offset: 3px;
	}
	.who small {
		display: block;
		color: var(--t2);
		font-size: 12.5px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.acct.problem .who small {
		color: color-mix(in srgb, var(--red) 55%, var(--text));
	}
	.who:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
		border-radius: 4px;
	}
	.row > .note {
		font-size: 12px;
		white-space: nowrap;
	}
	.chev .i {
		transition: transform 0.15s;
	}
	.chev.down .i {
		transform: rotate(90deg);
	}
	.toast {
		position: fixed;
		left: 50%;
		bottom: 20px;
		transform: translateX(-50%);
		z-index: 60;
		display: flex;
		align-items: center;
		gap: 12px;
		max-width: calc(100vw - 32px);
		padding: 10px 14px;
		border-radius: 8px;
		border: 1px solid var(--border);
		background: var(--surface-2);
		box-shadow: 0 10px 24px rgb(0 0 0 / 0.4);
		font-size: 13px;
	}

	/* Shared bits for the variant partials. */
	.ap :global(.panel) {
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
	}
	.ap :global(.row) {
		display: flex;
		align-items: center;
		gap: 14px;
		padding: 12px 16px;
		min-width: 0;
	}
	.ap :global(.row + .row),
	.ap :global(.row + .exp),
	.ap :global(.exp + .row) {
		border-top: 1px solid var(--border-soft);
	}
	.ap :global(.i) {
		width: 14px;
		height: 14px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.8;
		stroke-linecap: round;
		stroke-linejoin: round;
		flex: none;
	}
	.ap :global(.av) {
		width: 32px;
		height: 32px;
		border-radius: 50%;
		display: grid;
		place-items: center;
		flex: none;
		font-size: 12px;
		font-weight: 600;
		background: var(--surface-3);
		color: var(--text);
		position: relative;
	}
	.ap :global(.av .mk) {
		position: absolute;
		right: -3px;
		bottom: -3px;
		display: grid;
		border-radius: 4px;
		box-shadow: 0 0 0 2px var(--surface);
	}
	.ap :global(.btn) {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: 6px;
		font: inherit;
		font-size: 13px;
		font-weight: 500;
		padding: 5px 12px;
		border-radius: 7px;
		border: 1px solid var(--border);
		background: var(--surface-2);
		color: var(--text);
		white-space: nowrap;
		cursor: pointer;
	}
	.ap :global(.btn:hover:not(:disabled)) {
		border-color: var(--t2);
	}
	.ap :global(.btn:disabled) {
		opacity: 0.5;
		cursor: default;
	}
	.ap :global(.btn.primary) {
		background: var(--accent-strong);
		border-color: var(--accent-strong);
		color: #fff;
	}
	.ap :global(.btn.primary:hover:not(:disabled)) {
		background: color-mix(in srgb, var(--accent-strong) 85%, #fff);
	}
	.ap :global(.btn.danger) {
		color: var(--red);
		border-color: color-mix(in srgb, var(--red) 45%, var(--border));
		background: var(--surface-2);
	}
	.ap :global(.btn.danger:hover:not(:disabled)) {
		border-color: var(--red);
		background: var(--red-soft);
	}
	.ap :global(.btn.danger.solid) {
		background: var(--red);
		border-color: var(--red);
		color: #fff;
	}
	.ap :global(.ico) {
		width: 30px;
		height: 30px;
		display: grid;
		place-items: center;
		flex: none;
		border: 0;
		border-radius: 6px;
		background: none;
		color: var(--t2);
		cursor: pointer;
	}
	.ap :global(.ico:hover:not(:disabled)) {
		background: var(--surface-3);
		color: var(--text);
	}
	.ap :global(.ico:disabled) {
		opacity: 0.35;
		cursor: default;
	}
	.ap :global(.lnk) {
		border: 0;
		background: none;
		padding: 0;
		font: inherit;
		color: var(--accent);
		text-decoration: underline;
		cursor: pointer;
	}
	.ap :global(a.lnk) {
		display: inline-flex;
		align-items: center;
		gap: 4px;
	}
	.ap :global(.btn:focus-visible),
	.ap :global(.ico:focus-visible),
	.ap :global(.lnk:focus-visible),
	.ap :global(.sw:focus-visible),
	.ap :global([role='tab']:focus-visible) {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.ap :global(.sw) {
		position: relative;
		width: 34px;
		height: 20px;
		border-radius: 10px;
		border: 1px solid var(--border);
		background: var(--surface-3);
		flex: none;
		padding: 0;
		cursor: pointer;
		transition:
			background 0.15s,
			border-color 0.15s;
	}
	.ap :global(.sw::after) {
		content: '';
		position: absolute;
		top: 2px;
		left: 2px;
		width: 14px;
		height: 14px;
		border-radius: 50%;
		background: var(--t2);
		transition:
			transform 0.15s,
			background 0.15s;
	}
	.ap :global(.sw[aria-checked='true']) {
		background: var(--accent-strong);
		border-color: var(--accent-strong);
	}
	.ap :global(.sw[aria-checked='true']::after) {
		transform: translateX(14px);
		background: #fff;
	}
	.ap :global(.inp) {
		height: 32px;
		padding: 0 10px;
		border: 1px solid var(--border);
		border-radius: 7px;
		background: var(--bg);
		color: var(--text);
		font: 13.5px var(--font);
		min-width: 0;
		width: 100%;
	}
	.ap :global(.inp:focus) {
		outline: 0;
		border-color: var(--accent);
		box-shadow: 0 0 0 3px var(--accent-soft);
	}
	.ap :global(.st) {
		font-size: 12px;
		font-weight: 500;
		padding: 2px 8px;
		border-radius: 10px;
		display: inline-flex;
		align-items: center;
		gap: 6px;
		white-space: nowrap;
		color: var(--t2);
		background: var(--surface-3);
	}
	.ap :global(.st::before) {
		content: '';
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: currentColor;
	}
	.ap :global(.st.mining),
	.ap :global(.st.force_watching) {
		color: var(--green);
		background: var(--green-soft);
	}
	.ap :global(.st.sleep_window) {
		color: var(--accent);
		background: var(--accent-soft);
	}
	.ap :global(.st.signed_out) {
		color: var(--red);
		background: var(--red-soft);
	}
	.ap :global(.st.paused) {
		color: var(--amber);
		background: var(--amber-soft);
	}
	.ap :global(.tag) {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		font-size: 12px;
		padding: 1px 8px;
		border-radius: 10px;
		color: var(--t2);
		background: var(--surface-3);
		white-space: nowrap;
	}
	.ap :global(.note) {
		font-size: 12.5px;
		margin: 0;
	}
	.ap :global(.note.ok) {
		color: var(--green);
	}
	.ap :global(.note.wait) {
		color: var(--t2);
	}
	.ap :global(.note.bad) {
		color: var(--red);
	}
	.ap :global(.sec) {
		display: grid;
		gap: 12px;
		min-width: 0;
	}
	.ap :global(.sh) {
		margin: 0;
		font-size: 11px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--t3);
	}
	.ap :global(.danger-zone .sh) {
		color: var(--red);
	}
	.ap :global(.opt) {
		display: flex;
		align-items: center;
		gap: 12px;
		min-width: 0;
	}
	.ap :global(.lbl) {
		flex: 1;
		min-width: 0;
	}
	.ap :global(.lbl b) {
		display: flex;
		align-items: center;
		gap: 8px;
		font-weight: 500;
		font-size: 13.5px;
	}
	.ap :global(.lbl small) {
		display: block;
		color: var(--t2);
		font-size: 12.5px;
	}
	.ap :global(.lbl .lnk) {
		font-size: 12.5px;
	}
	.ap :global(.kv) {
		display: grid;
		grid-template-columns: max-content minmax(0, 1fr);
		gap: 6px 16px;
		margin: 0;
		font-size: 13px;
	}
	.ap :global(.kv dt) {
		color: var(--t3);
	}
	.ap :global(.kv dd) {
		margin: 0;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.ap :global(.acts) {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
	.ap :global(.mono) {
		font-family: var(--mono);
		font-variant-numeric: tabular-nums;
	}

	@media (max-width: 520px) {
		.ap :global(.kv) {
			grid-template-columns: minmax(0, 1fr);
			gap: 0;
		}
		.ap :global(.kv dd + dt) {
			margin-top: 8px;
		}
		.ap :global(.opt) {
			flex-wrap: wrap;
		}
		.ap :global(.opt > .lbl) {
			flex: 1 1 180px;
		}
		.ap :global(.opt > .btn),
		.ap :global(.opt > .acts) {
			margin-left: auto;
		}
	}
	@media (max-width: 720px) {
		.ap :global(.row) {
			flex-wrap: wrap;
			padding: 12px 14px;
			gap: 10px 12px;
		}
		.acct .who {
			flex: 1 1 calc(100% - 140px);
		}
		.acct :global(.menu-wrap) {
			order: 2;
		}
		.acct .chev {
			order: 3;
		}
		.acct .st {
			order: 4;
			margin-left: 44px;
		}
		.row > .note {
			order: 5;
			flex-basis: 100%;
			margin-left: 44px;
			white-space: normal;
		}
		.acct .fix {
			order: 6;
			width: 100%;
		}
	}
</style>
