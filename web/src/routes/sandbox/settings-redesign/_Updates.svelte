<script lang="ts">
	import Ico from './_Ico.svelte';
	import { st } from './_state.svelte';

	/** Colorful shows Up to date in the section head, so it can hide the row copy. */
	let { chip = true }: { chip?: boolean } = $props();

	let checking = $state(false);
	function check(): void {
		checking = true;
		setTimeout(() => (checking = false), 1200);
	}
</script>

<div class="panel">
	<div class="row" id="set-update-check">
		<div class="ver">
			<span>Current</span><b>v2.2.2</b>
		</div>
		<div class="ver">
			<span>Latest</span><b>v2.2.2</b>
		</div>
		<div class="lbl">{#if chip}<span class="chip ok">Up to date</span>{/if}</div>
		<button type="button" class="btn tint" disabled={checking} onclick={check}>
			<Ico name="refresh" size={13} />
			{checking ? 'Checking' : 'Check for updates'}
		</button>
	</div>
	<div class="card-body">
		<p class="help">No auto-update. Run this on your server.</p>
		<div class="cmd">
			<code>docker compose pull && docker compose up -d</code>
			<button type="button" class="btn ghost" aria-label="Copy command"><Ico name="copy" size={14} /></button>
		</div>
	</div>
	<div class="row" id="set-update-auto">
		<div class="lbl">
			<b>Check automatically</b>
			<small>Every hour. Checked 12 min ago.</small>
		</div>
		<button
			type="button"
			class="sw"
			role="switch"
			aria-checked={st.autoCheck}
			aria-label="Check automatically"
			onclick={() => ((st.autoCheck = !st.autoCheck), st.flash())}
		></button>
	</div>
</div>

<style>
	.ver {
		display: grid;
		gap: 0;
	}
	.ver span {
		font-size: 12px;
		color: var(--t2);
	}
	.ver b {
		font: 600 14px var(--mono);
	}
	.cmd {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 6px 6px 6px 12px;
		border-radius: 8px;
		background: var(--bg);
		border: 1px solid var(--border-soft);
		min-width: 0;
	}
	code {
		flex: 1;
		min-width: 0;
		font: 12.5px var(--mono);
		color: var(--hue);
		overflow-x: auto;
		white-space: nowrap;
	}
</style>
