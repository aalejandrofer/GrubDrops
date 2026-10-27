<script lang="ts">
	import Mock from './_Mock.svelte';
	import { variants } from './_variants';
</script>

<main>
	<p><a href="/sandbox/">Sandbox</a> / home-idle-accounts</p>
	<h1>Idle accounts on Home, four placements</h1>
	<p class="sub">
		GRUB-36. Static slice of Home with the prod case: Overwatch 2 is rank 1 and Twitch only, Phluses (Twitch) and TTik3r (Kick) have an
		own list of Rust and League of Legends. Full page per variant: {#each variants as v, i (v.id)}{i ? ', ' : ''}<a href={`/sandbox/home-idle-accounts/${v.id}`}
				>{v.id}</a
			>{/each}.
	</p>
	{#each variants as v (v.id)}
		<section>
			<h2>{v.name}</h2>
			<p class="note">{v.note}</p>
			<Mock variant={v} />
		</section>
	{/each}
</main>

<style>
	main {
		padding: 24px 20px 48px;
		background: #1c1c1f;
		color: #e6e6e6;
		min-height: 100vh;
	}
	a {
		text-decoration: underline;
	}
	h1 {
		margin: 4px 0;
		font-size: 22px;
	}
	.sub,
	.note {
		color: #b5b5bb;
		margin: 0 0 12px;
		max-width: 75ch;
	}
	section {
		margin-top: 32px;
	}
	h2 {
		margin: 0 0 2px;
		font-size: 16px;
	}
</style>
