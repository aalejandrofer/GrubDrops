<script lang="ts">
	type Mockup = {
		slug: string;
		created: string;
		title: string;
		desc: string;
		tags: string[];
		done?: boolean;
		cancelled?: boolean;
		chosen?: string;
	};

	const mockups: Mockup[] = [
		{
			slug: 'games-drop-priority',
			created: '2026-10-27',
			title: 'Drop priority switch on Games, three placements',
			desc: 'Where the Priority Order / Ending Soonest switch goes on Games so the game list starts near the top: rows above the list (A, B), next to the title (C), or in the rail (D, E, F). Each variant toggles Ending Soonest.',
			tags: ['games', 'ending soonest'],
			done: true,
			chosen: 'Variant F, a Priority Mode panel at the top of the rail; Cards | List beside Scan now'
		},
		{
			slug: 'home-drawer',
			created: '2026-10-27',
			title: 'Campaign drawer on Home, two click targets',
			desc: 'Opens the campaign drawer from a Home game card campaign and from an Up next row, with the clicked drop highlighted. The campaign name is the link, or a details button sits beside it so drag in Up next stays clear.',
			tags: ['home', 'GRUB-27'],
			done: true,
			chosen: 'Variant A, the campaign name is the link'
		},
		{
			slug: 'home-idle-accounts',
			created: '2026-10-27',
			title: 'Where idle accounts go on Home, four placements',
			desc: 'Idle, paused and signed-out accounts all pile under the rank 1 card today. A separate Not mining strip; each account under the best card it can mine; one quiet line under the last card, grouped by state; or the same line as chips. A Today page shows the current layout.',
			tags: ['home', 'GRUB-36'],
			done: true,
			chosen: 'Variant D, chips under the last card, with no Log in link'
		},
		{
			slug: 'inventory-upnext',
			created: '2026-10-27',
			title: 'Inventory Up next grouped by game, two ways to open a game',
			desc: "One Up next row per game in queue order, with drops added up across its campaigns. A expands the row in place; B opens a side drawer with all of that game's campaigns. Home Up next stays one row per drop.",
			tags: ['inventory', 'GRUB-29'],
			done: true,
			chosen: 'Variant A, the row expands in place'
		},
		{
			slug: 'home-force-watch',
			created: '2026-10-27',
			title: 'Force watch on Home, three placements',
			desc: 'Live forced channels and who is watching them, shown only when the Force watch list has a channel. A fourth KPI tile, a section above Up next, or a status next to the mining line.',
			tags: ['home', 'force watch', 'GRUB-24'],
			done: true,
			chosen: 'Variant A, as the third KPI tile before Accounts healthy'
		},
		{
			slug: 'force-watch',
			created: '2026-10-27',
			title: 'Force watch panel on Games, two add flows',
			desc: 'A Force watch panel under Lists on the Games rail. Paste a twitch.tv or kick.com link, pick the accounts that watch it when idle. Panel order decides. Adding in a dialog or inline in the panel.',
			tags: ['games', 'force watch', 'GRUB-24'],
			done: true,
			chosen: 'Variant A, add in a dialog'
		},
		{
			slug: 'campaign-detail',
			created: '2026-10-27',
			title: 'Campaign detail from Games and Rewards, three containers',
			desc: 'One view of a whole campaign: every drop, minutes, per-account claimed, progress or needs-link, clicked reward highlighted. Shown as a modal, a side drawer, or inline expand.',
			tags: ['games', 'rewards', 'GRUB-25', 'GRUB-26'],
			done: true,
			chosen: 'Variant B, side drawer on /games and /rewards'
		},
		{
			slug: 'account-games',
			created: '2026-10-26',
			title: 'Per-account game lists on Games, three layouts',
			desc: 'Where the own-list switch and editor live on Games: a section below the shared list with a modal, a left rail with a modal, or a left rail that swaps the main list.',
			tags: ['games', 'accounts'],
			done: true,
			chosen: 'Variant B, now live on /games as the Lists rail'
		},
		{
			slug: 'light-mode',
			created: '2026-10-26',
			title: 'Light mode, three darker options',
			desc: 'Light theme is too bright. Three token sets (dimmed off-white, warm paper, cool grey-blue) on a slice of the real UI, with AA contrast checks.',
			tags: ['theme', 'light mode', 'GRUB-22'],
			done: true,
			chosen: 'V1 Dimmed off-white, now the light theme in app.css'
		},
		{
			slug: 'login',
			created: '2026-10-26',
			title: 'Login and setup, three layouts',
			desc: 'Card, split pane and bare page. Each shows default, wrong password, SSO only, signing in, and first admin setup.',
			tags: ['auth', 'login', 'SSO'],
			done: true,
			chosen: 'Variant A, now live at /login'
		},
		{
			slug: 'onboarding',
			created: '2026-10-26',
			title: 'First-run onboarding, three flows',
			desc: 'Checklist on Home, guided wizard, inline empty states. Each walks add account, pick games, mining started. The live wizard has two steps: accounts (several allowed) and games.',
			tags: ['onboarding', 'first run'],
			done: true,
			chosen: 'Variant B, now live as the first-run wizard'
		}
	];
</script>

<main>
	<h1>Sandbox</h1>
	<p class="sub">Design mockups. Dev builds only.</p>
	<div class="grid">
		{#each mockups as m (m.slug)}
			<a class="card" href={`/sandbox/${m.slug}/`}>
				<div class="meta"><code>{m.slug}</code><time>{m.created}</time></div>
				<h2>{m.title}</h2>
				<p>{m.desc}</p>
				<div class="tags">
					{#each m.tags as tag, i (`${i}-${tag}`)}<span>{tag}</span>{/each}
					{#if m.done}<span class="ok">done</span>{/if}
					{#if m.cancelled}<span class="no">cancelled</span>{/if}
				</div>
				{#if m.chosen}<p class="chosen">{m.chosen}</p>{/if}
			</a>
		{/each}
	</div>
</main>

<style>
	main {
		max-width: 960px;
		margin: 0 auto;
		padding: 32px 20px;
	}
	h1 {
		margin: 0;
		font-size: 24px;
	}
	.sub {
		color: var(--t3);
		margin: 4px 0 24px;
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
		gap: 14px;
	}
	.card {
		display: block;
		padding: 16px;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
	}
	.card:hover {
		border-color: var(--accent);
	}
	.meta {
		display: flex;
		justify-content: space-between;
		font-size: 12px;
		color: var(--t3);
	}
	code {
		font-family: var(--mono);
	}
	h2 {
		margin: 8px 0 6px;
		font-size: 16px;
	}
	p {
		margin: 0;
		color: var(--t2);
	}
	.tags {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		margin-top: 12px;
	}
	.tags span {
		padding: 2px 8px;
		border-radius: 999px;
		background: var(--surface-2);
		color: var(--t2);
		font-size: 12px;
	}
	.tags .ok {
		background: var(--green-soft);
		color: var(--green);
	}
	.tags .no {
		background: var(--red-soft);
		color: var(--red);
	}
	.chosen {
		margin-top: 10px;
		font-size: 13px;
	}
</style>
