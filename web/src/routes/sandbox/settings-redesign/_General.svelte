<script lang="ts">
	import Ico from './_Ico.svelte';
	import { LANGS, ZONES } from './_data';
	import { st } from './_state.svelte';
</script>

<div class="panel">
	<div class="row" id="set-lang">
		<div class="lbl"><b>Language</b></div>
		<select class="sel" aria-label="Language" bind:value={st.lang} onchange={() => st.flash()}>
			{#each LANGS as l}<option value={l}>{l}</option>{/each}
		</select>
	</div>
	<div class="row" id="set-timefmt">
		<div class="lbl">
			<b>Time format</b>
			<small>{st.timefmt === '24h' ? '14:32' : '2:32 PM'}</small>
		</div>
		<div class="seg" role="group" aria-label="Time format">
			<button type="button" aria-pressed={st.timefmt === '24h'} onclick={() => ((st.timefmt = '24h'), st.flash())}>24h</button>
			<button type="button" aria-pressed={st.timefmt === '12h'} onclick={() => ((st.timefmt = '12h'), st.flash())}>12h</button>
		</div>
	</div>
	<div class="row" id="set-theme">
		<div class="lbl"><b>Theme</b></div>
		<div class="seg" role="group" aria-label="Theme">
			<button type="button" aria-pressed={st.theme === 'dark'} onclick={() => st.setTheme('dark')}>Dark</button>
			<button type="button" aria-pressed={st.theme === 'light'} onclick={() => st.setTheme('light')}>Light</button>
		</div>
	</div>
	<div class="row" id="set-tz">
		<div class="lbl"><b>Timezone</b></div>
		<select class="sel tz" aria-label="Timezone" bind:value={st.tz} onchange={() => st.flash()}>
			{#each ZONES as z}<option value={z}>{z}</option>{/each}
		</select>
	</div>
	<div class="row" id="set-signout">
		<div class="lbl">
			<b>Sign out</b>
			<small>Signed in as admin.</small>
		</div>
		<button type="button" class="btn"><Ico name="log-out" size={14} /> Sign out</button>
	</div>
</div>

<style>
	.tz {
		max-width: 260px;
	}
</style>
