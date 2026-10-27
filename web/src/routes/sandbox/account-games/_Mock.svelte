<script lang="ts">
	import { accounts as seed, shared, type Acct, type Variant } from './_variants';

	let { variant }: { variant: Variant } = $props();

	let accts = $state<Acct[]>(structuredClone(seed));
	let editing = $state<Acct | null>(null);
	let draft = $state<string[]>([]);
	let selected = $state<string | null>(null);

	const sharedCount = $derived(accts.filter((a) => !a.own).length);
	const current = $derived(accts.find((a) => a.id === selected) ?? null);

	function open(a: Acct) {
		editing = a;
		draft = a.games.length ? [...a.games] : [...shared];
	}
	function toggle(a: Acct) {
		a.own = !a.own;
		if (a.own && variant.id !== 'c') open(a);
		if (a.own && !a.games.length) a.games = [...shared];
	}
	function save() {
		if (editing) editing.games = [...draft];
		editing = null;
	}
	function move(list: string[], i: number, d: number) {
		const j = i + d;
		if (j < 0 || j >= list.length) return;
		[list[i], list[j]] = [list[j], list[i]];
	}
	const summary = (a: Acct) => (a.own ? `Own list · ${a.games.length} games` : 'Shared list');
</script>

{#snippet head()}
	<div class="ph">
		<h1>Games</h1>
		<span class="st">4 games · last scan 3m ago</span>
		<button type="button" class="btn sm">Scan now</button>
	</div>
{/snippet}

{#snippet search(placeholder: string)}
	<div class="search"><span class="mag">⌕</span>{placeholder}</div>
{/snippet}

{#snippet cards(list: string[], editable: boolean)}
	<div class="list">
		{#each list as g, i (g)}
			<article class="card">
				<div class="grip">
					<button type="button" class="mini" disabled={!editable || i === 0} onclick={() => move(list, i, -1)} aria-label="Move up">▴</button>
					<button type="button" class="mini" disabled={!editable || i === list.length - 1} onclick={() => move(list, i, 1)} aria-label="Move down">▾</button>
				</div>
				<div class="art"><span>{String(i + 1).padStart(2, '0')}</span></div>
				<div class="body">
					<b>{g}</b> <span class="chip">• No campaign</span>
					<p>Nothing live. Last scan 3m ago.</p>
				</div>
				{#if editable}<button type="button" class="rm">× Remove</button>{/if}
			</article>
		{/each}
	</div>
{/snippet}

{#snippet plat(a: Acct)}
	<span class="pl {a.platform}">{a.platform === 'twitch' ? 'T' : 'K'}</span>
{/snippet}

<div class="app">
	<nav class="top"><b>GrubDrops</b><span>Home</span><span>Discover</span><span class="on">Games</span><span>Rewards</span><span>Settings</span></nav>

	{#if variant.id === 'a'}
		<main class="page">
			{@render head()}
			{@render search('Add a game')}
			<div class="lh"><h2>Priority</h2><span>Top game mines first. Accounts on the shared list mine these.</span></div>
			{@render cards(shared, true)}

			<div class="lh sec"><h2>Per-account lists</h2><span>Off: the account mines the shared list above.</span></div>
			<ul class="accts">
				{#each accts as a (a.id)}
					<li>
						{@render plat(a)}
						<div class="who">
							<b>{a.name}</b>
							<small class:own={a.own}>{summary(a)}{#if a.own}: {a.games.slice(0, 3).join(', ')}{/if}</small>
						</div>
						{#if a.own}<button type="button" class="btn sm" onclick={() => open(a)}>Edit list</button>{/if}
						<button type="button" class="sw" role="switch" aria-checked={a.own} aria-label={`Own list for ${a.name}`} onclick={() => toggle(a)}></button>
					</li>
				{/each}
			</ul>
		</main>
	{:else}
		<main class="page wide">
			{@render head()}
			<div class="cols">
				<aside class="rail">
					<p class="rh">Lists</p>
					<button type="button" class="ri" class:sel={variant.id === 'c' && !current} onclick={() => (selected = null)}>
						<span class="pl all">★</span>
						<span class="who"><b>Shared list</b><small>{shared.length} games · {sharedCount} accounts</small></span>
					</button>
					<p class="rh">Accounts</p>
					{#each accts as a (a.id)}
						<div class="ri" class:sel={variant.id === 'c' && selected === a.id}>
							{#if variant.id === 'c'}
								<button type="button" class="pick" onclick={() => (selected = a.id)}>
									{@render plat(a)}<span class="who"><b>{a.name}</b><small class:own={a.own}>{summary(a)}</small></span>
								</button>
							{:else}
								{@render plat(a)}
								<span class="who">
									<b>{a.name}</b>
									{#if a.own}<small class="own">{a.games.length} games · <button type="button" class="lnk" onclick={() => open(a)}>Edit list</button></small>
									{:else}<small>{summary(a)}</small>{/if}
								</span>
								<button type="button" class="sw" role="switch" aria-checked={a.own} aria-label={`Own list for ${a.name}`} onclick={() => toggle(a)}></button>
							{/if}
						</div>
					{/each}
				</aside>

				<section class="main">
					{#if variant.id === 'c' && current}
						<div class="ownbar">
							<div><b>{current.name}</b><small>{current.own ? 'Mines its own list, ranked below.' : 'Mines the shared list.'}</small></div>
							<label class="swl">Use own game list
								<button type="button" class="sw" role="switch" aria-checked={current.own} aria-label="Use own game list" onclick={() => toggle(current)}></button>
							</label>
						</div>
						{#if current.own}
							{@render search(`Add a game to ${current.name}`)}
							<div class="lh"><h2>Priority</h2><span>{current.name} only.</span></div>
							{@render cards(current.games, true)}
						{:else}
							<div class="lh"><h2>Shared list</h2><span>Read-only here. Edit it under Shared list.</span></div>
							{@render cards(shared, false)}
						{/if}
					{:else}
						{@render search('Add a game')}
						<div class="lh"><h2>Priority</h2><span>Top game mines first. Accounts on the shared list mine these.</span></div>
						{@render cards(shared, true)}
					{/if}
				</section>
			</div>
		</main>
	{/if}

	{#if editing}
		<div class="scrim" role="presentation" onclick={() => (editing = null)}></div>
		<div class="modal" role="dialog" aria-modal="true" aria-label="Game list">
			<header><h3>Game list · {editing.name}</h3><button type="button" class="mini" onclick={() => (editing = null)} aria-label="Close">×</button></header>
			<p class="mhint">Only these games, top first. Starts as a copy of the shared list.</p>
			{@render search('Add a game')}
			<ol class="gl">
				{#each draft as g, i (g)}
					<li>
						<span class="n">{i + 1}</span><span class="nm">{g}</span>
						<button type="button" class="mini" disabled={i === 0} onclick={() => move(draft, i, -1)} aria-label="Up">▴</button>
						<button type="button" class="mini" disabled={i === draft.length - 1} onclick={() => move(draft, i, 1)} aria-label="Down">▾</button>
						<button type="button" class="mini" onclick={() => (draft = draft.filter((x) => x !== g))} aria-label="Remove">×</button>
					</li>
				{/each}
			</ol>
			<footer><button type="button" class="btn" onclick={() => (editing = null)}>Cancel</button><button type="button" class="btn pri" onclick={save}>Save</button></footer>
		</div>
	{/if}
</div>

<style>
	.app {
		position: relative;
		background: var(--bg);
		color: var(--text);
		font: 14px var(--font);
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		overflow: hidden;
		min-height: 760px;
	}
	.top {
		display: flex;
		gap: 22px;
		align-items: center;
		padding: 14px 24px;
		border-bottom: 1px solid var(--border-soft);
		color: var(--t2);
	}
	.top b {
		color: var(--text);
		margin-right: 16px;
	}
	.top .on {
		color: var(--text);
		background: var(--surface-2);
		padding: 4px 10px;
		border-radius: 6px;
	}
	.page {
		max-width: 1000px;
		margin: 0 auto;
		padding: 24px;
	}
	.page.wide {
		max-width: 1180px;
	}
	.ph {
		display: flex;
		align-items: center;
		gap: 14px;
		margin-bottom: 18px;
	}
	.ph h1 {
		margin: 0;
		font-size: 19px;
	}
	.st {
		color: var(--amber);
		font-size: 13px;
	}
	.ph .btn {
		margin-left: auto;
	}
	.search {
		border: 1px solid var(--border);
		background: var(--surface);
		border-radius: 8px;
		padding: 10px 14px;
		color: var(--t3);
		margin-bottom: 18px;
	}
	.mag {
		margin-right: 8px;
	}
	.lh {
		display: flex;
		gap: 10px;
		align-items: baseline;
		margin: 6px 0 10px;
	}
	.lh.sec {
		margin-top: 30px;
	}
	.lh h2 {
		margin: 0;
		font-size: 14px;
	}
	.lh span {
		color: var(--t2);
		font-size: 13px;
	}
	.list {
		display: grid;
		gap: 8px;
	}
	.card {
		display: flex;
		align-items: center;
		gap: 14px;
		border: 1px solid var(--border-soft);
		background: var(--surface);
		border-radius: 10px;
		overflow: hidden;
		padding-right: 18px;
	}
	.grip {
		display: grid;
		padding-left: 6px;
	}
	.art {
		width: 96px;
		height: 64px;
		background: var(--surface-3);
		position: relative;
	}
	.art span {
		position: absolute;
		left: 6px;
		bottom: 4px;
		font: 12px var(--mono);
	}
	.body {
		flex: 1;
	}
	.body p {
		margin: 4px 0 0;
		color: var(--t2);
		font-size: 13px;
	}
	.chip {
		font-size: 12px;
		color: var(--t2);
		background: var(--surface-2);
		border-radius: 999px;
		padding: 2px 8px;
		margin-left: 6px;
	}
	.rm {
		background: none;
		border: 0;
		color: var(--t2);
		font: inherit;
		font-size: 13px;
	}
	.accts {
		list-style: none;
		margin: 0;
		padding: 0;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
	}
	.accts li {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 12px 16px;
	}
	.accts li + li {
		border-top: 1px solid var(--border-soft);
	}
	.who {
		flex: 1;
		min-width: 0;
		display: grid;
		text-align: left;
	}
	.who small {
		color: var(--t3);
		font-size: 12px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.who small.own,
	.lnk {
		color: var(--accent);
	}
	.lnk {
		text-decoration: underline;
		background: none;
		border: 0;
		padding: 0;
		font: inherit;
		font-size: 12px;
		text-align: left;
		cursor: pointer;
	}
	.pl {
		width: 26px;
		height: 26px;
		border-radius: 6px;
		display: grid;
		place-items: center;
		font-size: 12px;
		font-weight: 700;
		flex: none;
	}
	.pl.twitch {
		background: var(--twitch);
		color: #fff;
	}
	.pl.kick {
		background: var(--kick);
		color: #000;
	}
	.pl.all {
		background: var(--accent-soft);
		color: var(--accent);
	}
	.cols {
		display: grid;
		grid-template-columns: 270px 1fr;
		gap: 22px;
		align-items: start;
	}
	.rail {
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		padding: 8px;
		display: grid;
		gap: 2px;
		position: sticky;
		top: 12px;
	}
	.rh {
		margin: 8px 8px 4px;
		font-size: 11px;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--t3);
	}
	.ri {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 8px;
		border-radius: 8px;
		border: 0;
		background: none;
		color: inherit;
		font: inherit;
		width: 100%;
		cursor: default;
	}
	button.ri,
	.pick {
		cursor: pointer;
	}
	.pick {
		display: flex;
		align-items: center;
		gap: 10px;
		flex: 1;
		min-width: 0;
		background: none;
		border: 0;
		color: inherit;
		font: inherit;
		padding: 0;
	}
	.ri.sel {
		background: var(--surface-2);
		box-shadow: inset 2px 0 0 var(--accent);
	}
	.ownbar {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 16px;
		border: 1px solid var(--border-soft);
		background: var(--surface);
		border-radius: 10px;
		padding: 12px 16px;
		margin-bottom: 18px;
	}
	.ownbar small {
		display: block;
		color: var(--t2);
		font-size: 12.5px;
	}
	.swl {
		display: flex;
		align-items: center;
		gap: 10px;
		font-size: 13px;
		color: var(--t2);
	}
	.btn {
		display: inline-flex;
		align-items: center;
		font: inherit;
		font-size: 13px;
		font-weight: 500;
		padding: 5px 12px;
		border-radius: 7px;
		border: 1px solid var(--border);
		background: var(--surface-2);
		color: var(--text);
		cursor: pointer;
		white-space: nowrap;
	}
	.btn.sm {
		padding: 3px 10px;
	}
	.btn.pri {
		background: var(--accent-strong);
		border-color: var(--accent-strong);
		color: #fff;
	}
	.sw {
		position: relative;
		width: 34px;
		height: 20px;
		border-radius: 10px;
		border: 1px solid var(--border);
		background: var(--surface-3);
		flex: none;
		padding: 0;
		cursor: pointer;
	}
	.sw::after {
		content: '';
		position: absolute;
		top: 2px;
		left: 2px;
		width: 14px;
		height: 14px;
		border-radius: 50%;
		background: var(--t2);
		transition: transform 0.15s;
	}
	.sw[aria-checked='true'] {
		background: var(--accent-strong);
		border-color: var(--accent-strong);
	}
	.sw[aria-checked='true']::after {
		transform: translateX(14px);
		background: #fff;
	}
	.mini {
		width: 24px;
		height: 22px;
		display: grid;
		place-items: center;
		padding: 0;
		border: 1px solid transparent;
		border-radius: 5px;
		background: none;
		color: var(--t2);
		cursor: pointer;
		flex: none;
		font-size: 13px;
	}
	.mini:disabled {
		opacity: 0.3;
		cursor: default;
	}
	.scrim {
		position: absolute;
		inset: 0;
		background: rgb(0 0 0 / 0.55);
	}
	.modal {
		position: absolute;
		top: 90px;
		left: 50%;
		transform: translateX(-50%);
		width: min(480px, calc(100% - 32px));
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: 12px;
		padding: 18px 20px;
		box-shadow: 0 20px 60px rgb(0 0 0 / 0.5);
	}
	.modal header {
		display: flex;
		justify-content: space-between;
		align-items: center;
	}
	.modal h3 {
		margin: 0;
		font-size: 16px;
	}
	.mhint {
		color: var(--t2);
		font-size: 13px;
		margin: 6px 0 14px;
	}
	.modal .search {
		margin-bottom: 0;
	}
	.gl {
		list-style: none;
		margin: 12px 0 0;
		padding: 0;
		border: 1px solid var(--border-soft);
		border-radius: 8px;
	}
	.gl li {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 6px 8px 6px 10px;
		font-size: 13px;
	}
	.gl li + li {
		border-top: 1px solid var(--border-soft);
	}
	.n {
		width: 18px;
		font: 12px var(--mono);
		color: var(--t3);
		text-align: right;
	}
	.nm {
		flex: 1;
	}
	.modal footer {
		display: flex;
		justify-content: flex-end;
		gap: 8px;
		margin-top: 18px;
	}
	@media (max-width: 760px) {
		.cols {
			grid-template-columns: 1fr;
		}
		.rail {
			position: static;
		}
	}
</style>
