<script lang="ts">
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { t } from '$lib/i18n/index.svelte';
	import Shell from './_Shell.svelte';
	import { fake, type Step } from './_variants';

	let { step }: { step: Step } = $props();
	const run = fake.runs[0];
	const chosen = fake.games.slice(0, 2);
	const titles = [t('home.step1_title'), t('home.step2_title'), 'Mining started'];
</script>

<Shell tabs={false}>
	<div class="wiz">
		<div class="prog">
			<span class="mono faint">Step {step} of 3</span>
			<ol class="segs" aria-label="Progress">
				{#each titles as name, i (name)}
					<li class:done={i + 1 < step || step === 3} class:cur={i + 1 === step && step !== 3}><span class="sr">{name}</span></li>
				{/each}
			</ol>
		</div>

		<h1>{titles[step - 1]}</h1>

		{#if step === 1}
			<p class="muted lead">{t('home.step1_body')}</p>
			<div class="tiles" role="radiogroup" aria-label="Platform">
				<button type="button" class="tile on" role="radio" aria-checked="true">
					<PlatformMark platform="twitch" /><b>{t('accounts.twitch')}</b><span class="faint">Android login or TV code</span>
				</button>
				<button type="button" class="tile" role="radio" aria-checked="false">
					<PlatformMark platform="kick" /><b>{t('accounts.kick')}</b><span class="faint">{t('accounts.cookies_label')} from your browser</span>
				</button>
			</div>

			<div class="tabs2" role="tablist" aria-label="Twitch sign-in">
				<button type="button" role="tab" aria-selected="false">Android login</button>
				<button type="button" role="tab" aria-selected="true" class="on">{t('accounts.method_tv')}</button>
			</div>
			<div class="code panel">
				<p>{t('accounts.device_go')}</p>
				<div class="code-row">
					<span class="big mono">{fake.code}</span>
					<button type="button" class="btn">Copy</button>
				</div>
				<p class="wait faint"><span class="spin" aria-hidden="true"></span>{t('accounts.device_waiting')}</p>
			</div>
		{:else if step === 2}
			<p class="muted lead">Search for a game. Top of the list mines first.</p>
			<label class="search">
				<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg>
				<input type="search" value="Apex" aria-label={t('home.search_label')} placeholder={t('home.search_ph')} />
			</label>
			<ul class="sugg">
				<li><span class="art" aria-hidden="true">A</span><b class="grow">Apex Legends</b><button type="button" class="btn">{t('home.add')}</button></li>
			</ul>

			<p class="lbl list-h">Mining order</p>
			<ol class="order panel">
				{#each chosen as g, i (g.name)}
					<li>
						<span class="mono rank">{i + 1}</span>
						<span class="art" aria-hidden="true">{g.name[0]}</span>
						<div class="grow">
							<b>{g.name}</b>
							<span class="faint meta"><PlatformMark platform={g.platform} size="sm" />{g.campaign} · {g.drops} drops · ends {g.ends}</span>
						</div>
						<span class="moves">
							<button type="button" class="ico" aria-label={`Move ${g.name} up`} disabled={i === 0}
								><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m6 15 6-6 6 6" /></svg></button
							>
							<button type="button" class="ico" aria-label={`Move ${g.name} down`} disabled={i === chosen.length - 1}
								><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg></button
							>
							<button type="button" class="ico" aria-label={`Remove ${g.name}`}
								><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12" /></svg></button
							>
						</span>
					</li>
				{/each}
			</ol>
		{:else}
			<p class="muted lead">1 account, 2 games. Each drop is claimed when its time is reached.</p>
			<div class="panel runp">
				<div class="run">
					<PlatformMark platform={run.platform} />
					<b class={`pf-name ${run.platform}`}>{run.account}</b>
					<span class="chip green">{t('accounts.status.mining')}</span>
				</div>
				<p class="muted line">{t('accounts.mining_line', { game: run.game, drop: run.drop, channel: run.channel, m: run.m, req: run.req })}</p>
				<div class="bar" aria-hidden="true"><i style={`width:${(run.m / run.req) * 100}%`}></i></div>
			</div>
		{/if}

		<div class="foot">
			{#if step > 1 && step < 3}<button type="button" class="btn quiet">Back</button>{/if}
			<span class="grow"></span>
			{#if step === 1}
				<button type="button" class="btn primary" disabled>Continue</button>
			{:else if step === 2}
				<button type="button" class="btn primary">Start mining</button>
			{:else}
				<a class="btn primary" href="#home">Open Home</a>
			{/if}
		</div>
	</div>
</Shell>

<style>
	.wiz {
		max-width: 560px;
		margin: 0 auto;
		padding-top: 12px;
	}
	.prog {
		display: flex;
		align-items: center;
		gap: 14px;
		margin-bottom: 22px;
	}
	.prog .mono {
		font-size: 12px;
		white-space: nowrap;
	}
	.segs {
		flex: 1;
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		gap: 4px;
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.segs li {
		height: 4px;
		border-radius: 2px;
		background: var(--surface-3);
	}
	.segs li.cur {
		background: var(--accent);
	}
	.segs li.done {
		background: var(--green);
	}
	.sr {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		clip-path: inset(50%);
	}
	h1 {
		font-size: 22px !important;
		letter-spacing: -0.02em !important;
	}
	.lead {
		margin: 4px 0 20px;
		font-size: 13.5px;
		max-width: 62ch;
	}
	.tiles {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 10px;
	}
	.tile {
		display: grid;
		grid-template-columns: auto 1fr;
		align-items: center;
		gap: 2px 10px;
		text-align: left;
		font: inherit;
		color: var(--text);
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: 10px;
		padding: 12px 14px;
		cursor: pointer;
	}
	.tile span {
		grid-column: 2;
		font-size: 12px;
	}
	.tile.on {
		border-color: var(--accent);
		background: var(--accent-soft);
	}
	.tile:focus-visible,
	.tabs2 button:focus-visible,
	.ico:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.tabs2 {
		display: flex;
		gap: 18px;
		margin: 22px 0 0;
		border-bottom: 1px solid var(--border-soft);
	}
	.tabs2 button {
		font: inherit;
		font-size: 13px;
		background: none;
		border: 0;
		color: var(--t2);
		padding: 0 0 8px;
		cursor: pointer;
	}
	.tabs2 .on {
		color: var(--text);
		font-weight: 500;
		box-shadow: inset 0 -2px 0 var(--accent);
	}
	.code {
		margin-top: 14px;
		padding: 16px 18px;
	}
	.code p {
		margin: 0;
		font-size: 13px;
	}
	.code-row {
		display: flex;
		align-items: center;
		gap: 14px;
		margin: 10px 0 12px;
	}
	.big {
		font-size: 28px;
		font-weight: 600;
		letter-spacing: 0.08em;
		color: var(--text);
	}
	.wait {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.list-h {
		margin: 22px 0 6px;
	}
	.sugg b,
	.order b {
		font-weight: 500;
	}
	.order {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.order li {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 10px 12px;
	}
	.order li + li {
		border-top: 1px solid var(--border-soft);
	}
	.rank {
		width: 14px;
		color: var(--accent);
		font-size: 13px;
		font-weight: 600;
	}
	.order .grow {
		display: grid;
	}
	.meta {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font-size: 12px;
	}
	.moves {
		display: flex;
		gap: 2px;
	}
	.ico {
		width: 28px;
		height: 28px;
		display: grid;
		place-items: center;
		background: none;
		border: 0;
		border-radius: 6px;
		color: var(--t2);
		cursor: pointer;
	}
	.ico:hover:not(:disabled) {
		background: var(--surface-2);
		color: var(--text);
	}
	.ico:disabled {
		opacity: 0.35;
		cursor: default;
	}
	.ico svg {
		width: 15px;
		height: 15px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.8;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
	.runp {
		padding: 14px 16px 16px;
	}
	.run {
		display: flex;
		align-items: center;
		gap: 10px;
	}
	.run .chip {
		margin-left: auto;
	}
	.line {
		margin: 6px 0 10px;
		font-size: 13px;
	}
	.foot {
		display: flex;
		align-items: center;
		gap: 8px;
		margin-top: 28px;
		padding-top: 16px;
		border-top: 1px solid var(--border-soft);
	}
	@container (max-width: 560px) {
		.tiles {
			grid-template-columns: 1fr;
		}
		.meta {
			display: none;
		}
	}
</style>
