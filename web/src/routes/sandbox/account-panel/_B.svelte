<script lang="ts">
	import type { PanelState } from './_state.svelte';
	import Shell from './_Shell.svelte';
	import Head from './_Head.svelte';
	import Login from './_Login.svelte';
	import Mining from './_Mining.svelte';
	import Points from './_Points.svelte';
	import Danger from './_Danger.svelte';

	/** B: the row opens a centered modal, one tab per section. */
	let { st }: { st: PanelState } = $props();
	const a = $derived(st.get(st.open));

	type Tab = 'login' | 'mining' | 'points' | 'danger';
	const TABS: { id: Tab; label: string }[] = [
		{ id: 'mining', label: 'Mining' },
		{ id: 'login', label: 'Login' },
		{ id: 'points', label: 'Channel points' },
		{ id: 'danger', label: 'Danger' }
	];
	let tab = $state<Tab>('mining');
	let dlg = $state<HTMLDialogElement | null>(null);
	let lastId = '';

	$effect(() => {
		const id = st.open;
		if (id && id !== lastId) {
			const acct = st.get(id);
			// An expired session opens on Login, where the fix is.
			if (acct?.needs_login) tab = 'login';
			else if (!lastId) tab = 'mining';
		}
		lastId = id ?? '';
		if (!dlg) return;
		if (id && !dlg.open) {
			dlg.showModal();
			// Start focus on the dialog, not the first button in it.
			dlg.focus();
		}
		else if (!id && dlg.open) dlg.close();
	});

	function keyTabs(e: KeyboardEvent): void {
		if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return;
		const i = TABS.findIndex((x) => x.id === tab);
		tab = TABS[(i + (e.key === 'ArrowRight' ? 1 : -1) + TABS.length) % TABS.length].id;
		(e.currentTarget as HTMLElement).querySelector<HTMLElement>(`[aria-selected='true']`)?.focus();
	}
</script>

<Shell {st}>
	<dialog
		bind:this={dlg}
		tabindex="-1"
		aria-label={a ? `${a.name} details` : 'Account details'}
		onclose={() => st.close()}
		onclick={(e) => e.target === dlg && st.close()}
	>
		{#if a}
			<div class="top">
				<Head {st} {a} />
				<div class="nav">
					<button type="button" class="ico" aria-label="Previous account" onclick={() => st.step(-1)}>
						<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="m15 6-6 6 6 6" /></svg>
					</button>
					<button type="button" class="ico" aria-label="Next account" onclick={() => st.step(1)}>
						<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="m9 6 6 6-6 6" /></svg>
					</button>
					<button type="button" class="ico" aria-label="Close details" onclick={() => st.close()}>
						<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12" /></svg>
					</button>
				</div>
			</div>
			<!-- svelte-ignore a11y_interactive_supports_focus -->
			<div class="tabs" role="tablist" aria-label="Sections" tabindex="-1" onkeydown={keyTabs}>
				{#each TABS as tb (tb.id)}
					<button
						type="button"
						role="tab"
						id="tb-{tb.id}"
						aria-selected={tab === tb.id}
						aria-controls="tp"
						tabindex={tab === tb.id ? 0 : -1}
						onclick={() => (tab = tb.id)}
					>
						{tb.label}
						{#if tb.id === 'login' && a.needs_login}<span class="dot" aria-label="needs attention"></span>{/if}
					</button>
				{/each}
			</div>
			<div class="body" role="tabpanel" id="tp" aria-labelledby="tb-{tab}">
				{#key a.id + tab}
					{#if tab === 'login'}<Login {st} {a} bare />
					{:else if tab === 'mining'}<Mining {st} {a} bare />
					{:else if tab === 'points'}<Points {a} bare />
					{:else}<Danger {st} {a} bare />{/if}
				{/key}
			</div>
			<div class="foot">
				<span>Changes save as you make them.</span>
				<button type="button" class="btn" onclick={() => st.close()}>Done</button>
			</div>
		{/if}
	</dialog>
</Shell>

<style>
	dialog {
		width: min(600px, calc(100vw - 32px));
		height: min(580px, calc(100dvh - 32px));
		padding: 0;
		border: 1px solid var(--border);
		border-radius: 12px;
		background: var(--bg);
		color: var(--text);
		overflow: hidden;
	}
	dialog:focus {
		outline: 0;
	}
	dialog[open] {
		display: flex;
		flex-direction: column;
	}
	dialog::backdrop {
		background: rgb(0 0 0 / 0.55);
	}
	.top {
		display: flex;
		gap: 12px;
		align-items: flex-start;
		padding: 20px 16px 14px 20px;
	}
	.top > :global(.hd) {
		flex: 1;
	}
	.nav {
		display: flex;
		gap: 2px;
		flex: none;
	}
	.tabs {
		display: flex;
		gap: 4px;
		padding: 0 16px;
		border-bottom: 1px solid var(--border-soft);
		overflow-x: auto;
		scrollbar-width: none;
		flex: none;
	}
	.tabs::-webkit-scrollbar {
		display: none;
	}
	.tabs button {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font: inherit;
		font-size: 13.5px;
		font-weight: 500;
		padding: 8px 10px;
		margin-bottom: -1px;
		border: 0;
		border-bottom: 2px solid transparent;
		background: none;
		color: var(--t2);
		cursor: pointer;
		white-space: nowrap;
	}
	.tabs button:hover {
		color: var(--text);
	}
	.tabs button[aria-selected='true'] {
		color: var(--text);
		border-bottom-color: var(--accent);
	}
	.dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--red);
	}
	.body {
		flex: 1;
		overflow-y: auto;
		padding: 20px;
	}
	.foot {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		padding: 10px 16px 10px 20px;
		border-top: 1px solid var(--border-soft);
		font-size: 12px;
		color: var(--t3);
		flex: none;
	}
	@media (max-width: 520px) {
		dialog {
			width: 100vw;
			max-width: 100vw;
			height: 100dvh;
			max-height: 100dvh;
			border: 0;
			border-radius: 0;
		}
		.top {
			padding: 14px 8px 10px 16px;
		}
		.top > :global(.hd .av) {
			display: none;
		}
		.tabs {
			padding: 0 8px;
		}
		.body {
			padding: 16px;
		}
		.foot {
			padding: 10px 16px;
		}
	}
</style>
