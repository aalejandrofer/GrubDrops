<script lang="ts">
	import type { PointsState } from './_state.svelte';

	let { st }: { st: PointsState } = $props();

	let url = $state('');
	let err = $state('');

	function submit(e: SubmitEvent): void {
		e.preventDefault();
		err = st.add(url);
		if (!err) url = '';
	}
</script>

<form class="add" onsubmit={submit}>
	<label>
		<span class="rh">Add a {st.tab === 'twitch' ? 'Twitch' : 'Kick'} channel</span>
		<span class="row">
			<input
				type="url"
				placeholder={st.tab === 'twitch' ? 'https://www.twitch.tv/keshaeuw' : 'https://kick.com/xqc'}
				bind:value={url}
				oninput={() => (err = '')}
			/>
			<button type="submit" class="btn primary" disabled={!url.trim()}>Add</button>
		</span>
	</label>
	{#if err}<p class="bad" role="alert">{err}</p>{:else}<p class="eg">Lands at the bottom with no accounts ticked.</p>{/if}
</form>

<style>
	.add {
		margin-top: 16px;
		max-width: 520px;
	}
	label {
		display: grid;
	}
	.row {
		display: flex;
		gap: 8px;
	}
	input {
		flex: 1;
		min-width: 0;
		font: inherit;
		font-size: 13px;
		height: 30px;
		padding: 0 10px;
		border-radius: 6px;
		border: 1px solid var(--border);
		background: var(--bg);
		color: var(--text);
	}
	.bad,
	.eg {
		margin: 6px 0 0;
		font-size: 12px;
	}
	.bad {
		color: var(--red);
	}
	.eg {
		color: var(--dim);
	}
</style>
