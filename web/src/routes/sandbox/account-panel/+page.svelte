<script lang="ts">
	import Theme from './_Theme.svelte';
	import { variants } from './_data';

	const LINE: Record<string, string> = {
		a: 'Row opens a drawer on the right with every section stacked.',
		b: 'Row opens a centered modal: header on top, one tab per section.',
		c: 'Row expands in place into a grid of section cards.'
	};
</script>

<main>
	<div class="bar">
		<p><a href="/sandbox/">Sandbox</a> / account-panel</p>
		<Theme />
	</div>
	<h1>Account detail view, three ways to open it</h1>
	<p class="sub">
		GRUB-50. Per-account options outgrew the ⋯ menu and the row badges, so each account gets a detail view opened from
		its row: header (name, platform, login type, status), Login, Mining (on/off, own games, skip sleep, Mine now),
		Channel points (read-only, set on <a href="/points">/points</a>) and Danger (restart, remove). The ⋯ menu keeps quick
		actions only. Static data: 3 Twitch accounts (mining, paused, expired login) and 2 Kick accounts. Toggles, rename,
		check login, log in again, Mine now, restart and remove all respond; nothing is saved.
	</p>
	<ul class="cards">
		{#each variants as v (v.id)}
			<li>
				<a class="card" href={`/sandbox/account-panel/${v.id}`}>
					<span class="id">{v.id.toUpperCase()}</span>
					<span class="txt">
						<b>{v.name.slice(2)}</b>
						<span class="line">{LINE[v.id]}</span>
						<span class="trade">{v.tradeoff}</span>
					</span>
				</a>
			</li>
		{/each}
	</ul>
</main>

<style>
	main {
		padding: 24px 20px 48px;
		min-height: 100vh;
		max-width: 900px;
		margin: 0 auto;
	}
	.sub a,
	.bar a {
		text-decoration: underline;
	}
	.bar {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}
	.bar p {
		margin: 0;
	}
	h1 {
		margin: 8px 0 4px;
		font-size: 22px;
	}
	.sub {
		color: var(--t2);
		margin: 0 0 20px;
		max-width: 80ch;
	}
	.cards {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 10px;
	}
	.card {
		display: flex;
		gap: 14px;
		align-items: flex-start;
		padding: 14px 16px;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
	}
	.card:hover {
		border-color: var(--accent);
	}
	.card:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.id {
		width: 32px;
		height: 32px;
		display: grid;
		place-items: center;
		flex: none;
		border-radius: 8px;
		background: var(--accent-soft);
		color: var(--accent);
		font-weight: 700;
	}
	.txt {
		display: grid;
		gap: 2px;
		min-width: 0;
	}
	.txt b {
		font-size: 15px;
		font-weight: 600;
	}
	.line {
		color: var(--text);
		font-size: 13.5px;
	}
	.trade {
		color: var(--t2);
		font-size: 13px;
	}
	@media (max-width: 720px) {
		main {
			padding: 16px 16px 48px;
		}
	}
</style>
