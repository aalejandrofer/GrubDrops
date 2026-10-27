<script lang="ts">
	import BoxArt from '$lib/components/BoxArt.svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import {
		accounts,
		games,
		groups,
		leftover,
		nMining,
		notMining,
		rowLine,
		stateLabel,
		under,
		why,
		type MockAcct,
		type MockGame,
		type Variant
	} from './_variants';

	let { variant }: { variant: Variant } = $props();

	const pct = (a: MockAcct) => `${((a.mining!.min / a.mining!.req) * 100).toFixed(1)}%`;
	const plat = (a: MockAcct) => (a.platform === 'kick' ? 'Kick' : 'Twitch');
</script>

{#snippet row(a: MockAcct, g: MockGame)}
	<div class="acct" class:out={a.state === 'signed_out' || a.state === 'paused'} class:idle={a.state === 'idle'}>
		<div class="who"><PlatformMark platform={a.platform} /><b>{a.name}</b></div>
		{#if a.mining}
			<div class="drop">
				<div class="l">
					<span>{a.mining.drop} <span class="ch">· {a.mining.channel}</span></span>
					<span>{a.mining.min}/{a.mining.req}</span>
				</div>
				<div class="bar"><i style:width={pct(a)}></i></div>
			</div>
			<div class="eta"><span>{a.mining.left}</span><small>left</small></div>
		{:else}
			<div class="drop">
				{rowLine(a, g)}
				{#if a.state === 'signed_out'}<a class="fix" href="/accounts">Log in</a>{/if}
			</div>
			<div class="eta"></div>
		{/if}
	</div>
{/snippet}

<div class="frame">
	<div class="head">
		<h1>Home</h1>
		<span class="now"><span class="dot"></span>Mining · {nMining} of {accounts.length} accounts</span>
		<span class="warn">1 account needs login</span>
	</div>

	<div class="kpis">
		<div class="kpi">
			<div class="label">Total Minutes Watched</div>
			<div class="val">12,480<small>min</small></div>
			<div class="sub">310 today</div>
		</div>
		<div class="kpi">
			<div class="label">Total Drops Claimed</div>
			<div class="val">142</div>
			<div class="sub">3 today</div>
		</div>
		<div class="kpi">
			<div class="label">Accounts Mining</div>
			<div class="val">{nMining}<small>/ {accounts.length}</small></div>
			<div class="sub">grubalt signed out</div>
		</div>
	</div>

	{#if variant.id === 'a'}
		<section class="strip" aria-labelledby="nm-{variant.id}">
			<div class="strip-h">
				<h2 id="nm-{variant.id}">Not mining</h2>
				<span class="n">{notMining.length}</span>
				<a class="lnk" href="/accounts">Accounts</a>
			</div>
			<ul>
				{#each notMining as a (a.id)}
					<li class={a.state}>
						<PlatformMark platform={a.platform} />
						<span class="nm"><b>{a.name}</b><small>{why(a)}</small></span>
						<span class="pill {a.state}">{stateLabel[a.state]}</span>
					</li>
				{/each}
			</ul>
		</section>
	{/if}

	<div class="layout">
		<div class="cards">
			<div class="sec-h"><h2>Your games</h2><span class="n">{games.length}</span></div>
			{#each games as g (g.id)}
				{@const list = under(variant.id, g)}
				<article class="game p{g.rank <= 2 ? g.rank : 3}">
					<div class="artw"><BoxArt src={g.art} name={g.name} size={g.rank <= 2 ? 'lg' : 'sm'} /></div>
					<div class="body">
						<div class="status">
							<h3 class="gname">{g.name}</h3>
							<span class="state {g.state}">{g.state === 'mining' ? 'Mining' : 'Waiting'}</span>
							<span class="camp">{g.camp}</span>
							<span class="plats">{g.platforms.map((p) => (p === 'kick' ? 'Kick' : 'Twitch')).join(' + ')} only</span>
						</div>
						{#if g.state !== 'mining'}
							<p class="idle-l">{g.waitLine}</p>
						{/if}
						{#if list.length}
							<div class="accts" class:after={g.state !== 'mining'}>
								{#each list as a (a.id)}
									{@render row(a, g)}
								{/each}
							</div>
						{/if}
					</div>
				</article>
			{/each}
			{#if variant.id === 'c'}
				<p class="quiet">
					{#each groups as gr, i (gr.state)}{i ? ' · ' : ''}<span class="grp"
							>{gr.accts.map((a) => a.name).join(', ')} {stateLabel[gr.state].toLowerCase()}{#if gr.state === 'signed_out'}<a
									class="fix"
									href="/accounts">Log in</a
								>{/if}</span
						>{/each}
				</p>
			{/if}
			{#if variant.id === 'd'}
				<ul class="chips" aria-label="Not mining">
					{#each groups as gr (gr.state)}
						{#each gr.accts as a (a.id)}
							<li class="chip">
								<PlatformMark platform={a.platform} />
								<span>{a.name}</span>
								<i class="sd {a.state}" title={stateLabel[a.state]}></i>
								<span class="st">{stateLabel[a.state]}</span>
							</li>
						{/each}
					{/each}
				</ul>
			{/if}
			{#if variant.id === 'b' && leftover.length}
				<p class="left">
					<span class="lh">No card fits</span>
					{#each leftover as a, i (a.id)}{i ? ', ' : ''}<span class="lw"
							><PlatformMark platform={a.platform} /><b>{a.name}</b> {stateLabel[a.state].toLowerCase()}</span
						>{/each}. No game on their lists has a {plat(leftover[0])} campaign.
				</p>
			{/if}
		</div>
		<aside>
			<section class="panel">
				<div class="panel-h"><h2>Up next</h2><span class="n">4</span></div>
				<div class="ph sm"></div>
				<div class="ph sm"></div>
			</section>
			<section class="panel">
				<div class="panel-h"><h2>Claimed</h2></div>
				<div class="ph sm"></div>
			</section>
		</aside>
	</div>
</div>

<style>
	.frame {
		padding: 24px;
		border: 1px solid var(--border);
		border-radius: 12px;
		background: var(--bg);
		color: var(--text);
	}
	.head {
		display: flex;
		align-items: center;
		gap: 8px 16px;
		flex-wrap: wrap;
		margin-bottom: 24px;
	}
	h1 {
		font-size: 18px;
		font-weight: 600;
		margin: 0;
	}
	.now {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		font-size: 13px;
		font-weight: 500;
		color: var(--green);
	}
	.warn {
		font-size: 12px;
		padding: 2px 10px;
		border-radius: 999px;
		border: 1px solid var(--amber);
		color: var(--amber);
		background: var(--amber-soft);
	}
	.dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: currentColor;
		flex: none;
	}
	.kpis {
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		border: 1px solid var(--border-soft);
		border-radius: var(--radius);
		background: var(--surface);
		margin-bottom: 20px;
	}
	.kpi {
		padding: 16px 20px 18px;
		min-width: 0;
	}
	.kpi + .kpi {
		border-left: 1px solid var(--border-soft);
	}
	.kpi .label {
		font-size: 12px;
		color: var(--t2);
	}
	.kpi .val {
		font-size: 30px;
		font-weight: 600;
		letter-spacing: -0.03em;
		line-height: 1.15;
		margin-top: 6px;
	}
	.kpi .val small {
		font-size: 14px;
		font-weight: 500;
		color: var(--t2);
		margin-left: 2px;
	}
	.kpi .sub {
		font-size: 12px;
		color: var(--t3);
		margin-top: 2px;
	}

	/* A: Not mining strip */
	.strip {
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		padding: 12px 14px;
		margin-bottom: 24px;
	}
	.strip-h {
		display: flex;
		align-items: center;
		gap: 8px;
		margin-bottom: 10px;
	}
	.strip-h h2,
	.sec-h h2 {
		margin: 0;
		font-size: 13px;
		font-weight: 600;
	}
	.n {
		font: 12px var(--mono);
		color: var(--t2);
		background: var(--surface-3);
		border-radius: 6px;
		padding: 1px 6px;
	}
	.lnk {
		margin-left: auto;
		font-size: 12px;
		color: var(--t2);
	}
	.lnk:hover {
		color: var(--text);
	}
	.strip ul {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(230px, 1fr));
		gap: 8px;
	}
	.strip li {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 8px 10px;
		border: 1px solid var(--border-soft);
		border-radius: 8px;
		background: var(--surface-2);
		min-width: 0;
	}
	.strip .nm {
		display: grid;
		min-width: 0;
	}
	.strip .nm b {
		font-size: 13px;
		font-weight: 500;
	}
	.strip .nm small {
		font-size: 12px;
		color: var(--t3);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.pill {
		margin-left: auto;
		font-size: 11px;
		font-weight: 500;
		padding: 2px 8px;
		border-radius: 10px;
		white-space: nowrap;
		color: var(--t2);
		background: var(--surface-3);
	}
	.pill.signed_out {
		color: var(--amber);
		background: var(--amber-soft);
	}

	.layout {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 320px;
		gap: 24px;
		align-items: start;
	}
	.cards,
	aside {
		display: grid;
		gap: 10px;
		min-width: 0;
	}
	.sec-h {
		display: flex;
		align-items: center;
		gap: 8px;
		margin-bottom: 2px;
	}

	/* Card: copied from GameCard.svelte */
	.game {
		display: grid;
		grid-template-columns: var(--aw) minmax(0, 1fr);
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		overflow: hidden;
	}
	.artw {
		position: relative;
		overflow: hidden;
		isolation: isolate;
		display: grid;
	}
	.body {
		padding: 16px 20px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		justify-content: center;
	}
	.p1,
	.p2 {
		--aw: 168px;
	}
	.p1 .artw {
		min-height: 200px;
	}
	.p1 .body {
		padding: 18px 20px;
		justify-content: flex-start;
	}
	.p2 .artw {
		min-height: 150px;
	}
	.p1 .gname,
	.p2 .gname {
		font-size: 18px;
	}
	.p3 {
		--aw: 80px;
	}
	.p3 .artw {
		min-height: 64px;
	}
	.p3 .body {
		padding: 12px 16px;
	}
	.p3 .gname {
		font-size: 14px;
	}
	.status {
		display: flex;
		align-items: center;
		gap: 6px 8px;
		flex-wrap: wrap;
		min-width: 0;
	}
	.gname {
		margin: 0 4px 0 0;
		font-weight: 600;
		letter-spacing: -0.01em;
		white-space: nowrap;
	}
	.state {
		font-size: 12px;
		font-weight: 500;
		padding: 2px 8px;
		border-radius: 10px;
		display: inline-flex;
		align-items: center;
		gap: 6px;
		white-space: nowrap;
	}
	.state::before {
		content: '';
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: currentColor;
	}
	.state.mining {
		color: var(--green);
		background: var(--green-soft);
	}
	.state.waiting {
		color: #b48cf8;
		background: var(--accent-soft);
	}
	.camp {
		color: var(--t2);
		font-size: 13px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		min-width: 0;
	}
	.plats {
		font-size: 11px;
		white-space: nowrap;
		color: var(--t3);
	}
	.idle-l {
		margin: 10px 0 0;
		color: var(--t2);
		font-size: 13px;
	}
	.accts {
		margin-top: 14px;
		display: grid;
		gap: 12px;
	}
	.accts.after {
		margin-top: 12px;
		padding-top: 12px;
		border-top: 1px solid var(--border-soft);
	}
	.acct {
		display: grid;
		grid-template-columns: 136px minmax(0, 1fr) 56px;
		gap: 14px;
		align-items: center;
	}
	.who {
		display: flex;
		align-items: center;
		gap: 7px;
		min-width: 0;
	}
	.who b {
		font-weight: 500;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.drop {
		min-width: 0;
	}
	.drop .l {
		display: flex;
		justify-content: space-between;
		gap: 8px;
		font-size: 13px;
	}
	.drop .l > span:first-child {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.drop .l > span:last-child {
		color: var(--t2);
		font: 12px var(--mono);
		white-space: nowrap;
	}
	.bar {
		height: 5px;
		border-radius: 3px;
		background: var(--surface-3);
		margin-top: 6px;
		overflow: hidden;
	}
	.bar i {
		display: block;
		height: 100%;
		border-radius: 3px;
		background: var(--accent);
	}
	.eta {
		text-align: right;
		font: 500 13px var(--mono);
	}
	.eta small {
		display: block;
		color: var(--t3);
		font: 12px var(--font);
	}
	.ch {
		margin-left: 4px;
		color: var(--t3);
		font-size: 12px;
	}
	.acct.out .who b,
	.acct.out .drop,
	.acct.idle .who b,
	.acct.idle .drop {
		color: var(--t2);
		font-size: 13px;
	}
	.fix {
		margin-left: 6px;
		color: var(--amber);
		font-size: 12px;
	}

	/* B: leftover line */
	.left {
		margin: 2px 2px 0;
		font-size: 12px;
		color: var(--t3);
		line-height: 1.6;
	}
	.lh {
		color: var(--t2);
		font-weight: 500;
		margin-right: 6px;
	}
	.lw {
		display: inline-flex;
		align-items: center;
		gap: 4px;
	}
	.lw b {
		color: var(--t2);
		font-weight: 500;
	}

	/* C: quiet line */
	.quiet {
		margin: 2px 2px 0;
		font-size: 12px;
		color: var(--t3);
		line-height: 1.6;
	}
	.grp {
		white-space: nowrap;
	}

	/* D: chips */
	.chips {
		list-style: none;
		margin: 2px 0 0;
		padding: 0;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px;
	}
	.chip {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 3px 9px 3px 5px;
		border: 1px solid var(--border-soft);
		border-radius: 999px;
		background: var(--surface);
		font-size: 12px;
		color: var(--t2);
	}
	.sd {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: var(--t3);
	}
	.sd.signed_out {
		background: var(--amber);
	}
	.sd.paused {
		background: transparent;
		box-shadow: inset 0 0 0 1.5px var(--t3);
	}
	.st {
		color: var(--t3);
	}

	.panel {
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		padding: 12px 14px;
	}
	.panel-h {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.panel-h h2 {
		margin: 0;
		font-size: 13px;
		font-weight: 600;
	}
	.ph.sm {
		height: 44px;
		margin-top: 8px;
		border-radius: 8px;
		background: var(--surface-2);
	}

	@media (max-width: 760px) {
		.frame {
			padding: 14px;
		}
		.kpis {
			grid-template-columns: 1fr 1fr;
		}
		.kpi:nth-child(3) {
			grid-column: 1 / -1;
			border-left: 0;
			border-top: 1px solid var(--border-soft);
		}
		.layout {
			grid-template-columns: minmax(0, 1fr);
		}
		.strip ul {
			grid-template-columns: minmax(0, 1fr);
		}
		.p1,
		.p2 {
			--aw: 84px;
		}
		.p3 {
			--aw: 52px;
		}
		.p1 .artw {
			min-height: 0;
		}
		.p1 .body,
		.p2 .body {
			padding: 14px 16px;
		}
		.p3 .body {
			padding: 10px 14px;
		}
		.camp {
			white-space: normal;
		}
		.acct {
			grid-template-columns: minmax(0, 1fr) auto;
			gap: 6px 12px;
		}
		.drop {
			grid-column: 1 / -1;
			grid-row: 2;
		}
		.eta small {
			display: inline;
			margin-left: 4px;
		}
	}
</style>
