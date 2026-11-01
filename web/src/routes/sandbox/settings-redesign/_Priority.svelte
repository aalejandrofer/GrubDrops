<script lang="ts">
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { SCANS } from './_data';
	import { st } from './_state.svelte';
</script>

<div class="panel">
	<div class="row" id="set-pm">
		<div class="lbl">
			<b>Priority mode</b>
			<small>{st.pm === 'ordered' ? 'Follows your game list order.' : 'Campaign ending first, across all games.'}</small>
		</div>
		<div class="seg" role="group" aria-label="Priority mode">
			<button type="button" aria-pressed={st.pm === 'ordered'} onclick={() => ((st.pm = 'ordered'), st.flash())}>Priority order</button>
			<button type="button" aria-pressed={st.pm === 'ending'} onclick={() => ((st.pm = 'ending'), st.flash())}>Ending soonest</button>
		</div>
	</div>
	<div class="row" id="set-preempt">
		<div class="lbl">
			<b>Switch to a higher-ranked drop</b>
			<small>{st.preempt ? 'Switches when a higher-ranked campaign goes live.' : 'Finishes the current drop first.'}</small>
		</div>
		<button
			type="button"
			class="sw"
			role="switch"
			aria-checked={st.preempt}
			aria-label="Switch to a higher-ranked drop"
			onclick={() => ((st.preempt = !st.preempt), st.flash())}
		></button>
	</div>
	<div class="row" id="set-raids">
		<div class="lbl">
			<b>Follow raids <PlatformMark platform="twitch" /></b>
			<small>Joins raids after a short random delay.</small>
		</div>
		<button
			type="button"
			class="sw"
			role="switch"
			aria-checked={st.raids}
			aria-label="Follow raids"
			onclick={() => ((st.raids = !st.raids), st.flash())}
		></button>
	</div>
	<div class="row" id="set-scan">
		<div class="lbl">
			<b>Check for new campaigns every</b>
			<small>Idle accounts recheck at this rate too.</small>
		</div>
		<select class="sel" aria-label="Check for new campaigns every" bind:value={st.scan} onchange={() => st.flash()}>
			{#each SCANS as m}<option value={m}>{m} min</option>{/each}
		</select>
	</div>
</div>
