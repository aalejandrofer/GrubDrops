<script lang="ts">
	import Mock from './_Mock.svelte';
	import { variants } from './_variants';
</script>

<main>
	<p><a href="/sandbox/">Sandbox</a> / add-game-hint</p>
	<h1>Add a game hint, one line instead of three</h1>
	<p class="sub">
		The "e.g." link, the "Campaign missing?" toggle and its paragraph become one hint line with a Twitch and a Kick link. The hint is one
		component (<code>$lib/components/Hint.svelte</code>: text, platform-tagged examples, inline, onpick), shown again in four other spots. Jandro picked C; its round 2 drops the hint text on Add a game and Points and links the cookies.txt extension. C is now live in the app. Full page per
		variant: {#each variants as v, i (v.id)}{i ? ', ' : ''}<a href={`/sandbox/add-game-hint/${v.id}`}>{v.id}</a>{/each}.
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
	code {
		font-family: var(--mono);
		font-size: 0.92em;
	}
	section {
		margin-top: 40px;
	}
	h2 {
		margin: 0 0 2px;
		font-size: 16px;
	}
	@media (max-width: 640px) {
		main {
			padding: 20px 16px 40px;
		}
	}
</style>
