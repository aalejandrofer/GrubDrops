<script lang="ts">
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { st } from './_state.svelte';

	const MODES = [
		{ id: 'auto', label: 'Auto', sub: 'WebSocket, falls back to browser if it keeps dropping.' },
		{ id: 'browser', label: 'Browser', sub: 'Plays the stream in a browser sidecar.' },
		{ id: 'ws', label: 'WebSocket', sub: 'WebSocket only, no browser.' }
	] as const;
	const mode = $derived(MODES.find((m) => m.id === st.kickMode) ?? MODES[0]);
</script>

<div class="panel">
	<div class="row" id="set-proxy">
		<div class="lbl">
			<b>Use a proxy <span class="chip off plain">After restart</span></b>
			<small>Routes all Twitch, Kick and notification traffic.</small>
		</div>
		<button
			type="button"
			class="sw"
			role="switch"
			aria-checked={st.proxyOn}
			aria-label="Use a proxy"
			onclick={() => ((st.proxyOn = !st.proxyOn), st.flash())}
		></button>
	</div>
	{#if st.proxyOn}
		<div class="card-body">
			<div class="inline">
				<div class="field grow">
					<label for="sx-proxy">Proxy URL</label>
					<input
						id="sx-proxy"
						class="inp"
						class:dirty={st.barMode && 'proxyUrl' in st.dirty}
						placeholder="socks5://user:pass@host:1080"
						value={st.proxyUrl}
						oninput={(e) => st.set('proxyUrl', e.currentTarget.value)}
					/>
				</div>
				<button type="button" class="btn">Test</button>
				{#if !st.barMode}<button type="button" class="btn" onclick={() => st.flash()}>Save</button>{/if}
			</div>
			<p class="help">http://, https:// or socks5://. Add user:pass@ if needed.</p>
		</div>
	{/if}
	<div class="row" id="set-kick-mode">
		<div class="lbl">
			<b>Kick watch mode <PlatformMark platform="kick" /></b>
			<small>{mode.sub}</small>
		</div>
		<div class="seg" role="group" aria-label="Kick watch mode">
			{#each MODES as m (m.id)}
				<button type="button" aria-pressed={st.kickMode === m.id} onclick={() => ((st.kickMode = m.id), st.flash())}>{m.label}</button>
			{/each}
		</div>
	</div>
</div>

<style>
	.grow {
		flex: 1;
	}
</style>
