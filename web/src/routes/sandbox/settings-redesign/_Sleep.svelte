<script lang="ts">
	import { st } from './_state.svelte';

	function mins(t: string): number {
		const [h, m] = t.split(':').map(Number);
		return (h || 0) * 60 + (m || 0);
	}
	/** The window drawn on a 24-hour strip, split in two when it crosses midnight. */
	const spans = $derived.by(() => {
		const a = mins(st.sleepStart) / 1440;
		const b = mins(st.sleepEnd) / 1440;
		return a <= b
			? [{ l: a, w: b - a }]
			: [
					{ l: a, w: 1 - a },
					{ l: 0, w: b }
				];
	});
</script>

<div class="panel">
	<div class="row" id="set-sleep-on">
		<div class="lbl">
			<b>Pause mining every night</b>
			<small>All accounts stop, Points watch too.</small>
		</div>
		<button
			type="button"
			class="sw"
			role="switch"
			aria-checked={st.sleepOn}
			aria-label="Pause mining every night"
			onclick={() => ((st.sleepOn = !st.sleepOn), st.flash())}
		></button>
	</div>
	<div class="card-body" id="set-sleep-times" class:off={!st.sleepOn}>
		<div class="strip" aria-hidden="true">
			{#each spans as s, i (i)}<span style="left: {s.l * 100}%; width: {s.w * 100}%"></span>{/each}
			<i style="left: 0">00</i><i style="left: 25%">06</i><i style="left: 50%">12</i><i style="left: 75%">18</i>
		</div>
		<div class="inline">
			<div class="field">
				<label for="sx-sleep-a">Start</label>
				<input
					id="sx-sleep-a"
					class="inp"
					class:dirty={st.barMode && 'sleepStart' in st.dirty}
					value={st.sleepStart}
					disabled={!st.sleepOn}
					oninput={(e) => st.set('sleepStart', e.currentTarget.value)}
				/>
			</div>
			<div class="field">
				<label for="sx-sleep-b">End</label>
				<input
					id="sx-sleep-b"
					class="inp"
					class:dirty={st.barMode && 'sleepEnd' in st.dirty}
					value={st.sleepEnd}
					disabled={!st.sleepOn}
					oninput={(e) => st.set('sleepEnd', e.currentTarget.value)}
				/>
			</div>
			{#if !st.barMode}<button type="button" class="btn" disabled={!st.sleepOn} onclick={() => st.flash()}>Save</button>{/if}
		</div>
		<p class="help">Times in Europe/Madrid. Can cross midnight. Skip sleep window accounts keep mining.</p>
	</div>
</div>

<style>
	.inline .field {
		width: 110px;
	}
	.strip {
		position: relative;
		height: 26px;
		border-radius: 6px;
		background: var(--bg);
		border: 1px solid var(--border-soft);
		overflow: hidden;
	}
	.strip span {
		position: absolute;
		top: 0;
		bottom: 0;
		background: color-mix(in srgb, var(--fill) 45%, transparent);
	}
	.strip i {
		position: absolute;
		bottom: 3px;
		font: 10.5px var(--mono);
		font-style: normal;
		color: var(--t3);
		padding-left: 4px;
		border-left: 1px solid var(--border);
	}
	.off .strip span {
		background: var(--surface-3);
	}
	.inp:disabled {
		color: var(--t3);
		background: var(--surface);
	}
</style>
