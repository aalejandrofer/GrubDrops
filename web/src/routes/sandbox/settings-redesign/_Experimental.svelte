<script lang="ts">
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { st } from './_state.svelte';

	/** TTik3r has an Android login, so the toggle is live. Without one it dims. */
	const android = $derived(st.accounts.some((a) => a.platform === 'twitch' && a.login === 'Android'));
</script>

<div class="panel">
	<div class="row" id="set-share" class:locked={!android}>
		<div class="lbl">
			<b>Share Android campaigns with TV accounts <PlatformMark platform="twitch" /> <span class="chip hue plain">Beta</span></b>
			<small>{android ? 'TV logins also mine campaigns an Android login can see.' : 'Needs an Android Twitch login.'}</small>
		</div>
		<button
			type="button"
			class="sw"
			role="switch"
			aria-checked={st.share}
			aria-label="Share Android campaigns with TV accounts"
			disabled={!android}
			onclick={() => ((st.share = !st.share), st.flash())}
		></button>
	</div>
</div>

<style>
	.locked .lbl {
		opacity: 0.6;
	}
</style>
