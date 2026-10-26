<script lang="ts">
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { t, tn } from '$lib/i18n/index.svelte';
	import Shell from './_Shell.svelte';
	import { fake, type Step } from './_variants';

	let { step }: { step: Step } = $props();
	const uid = $props.id();
	const run = fake.runs[0];
	const done = $derived(step - 1 + (step === 3 ? 1 : 0));
	const games = fake.games.filter((g) => g.platform === 'twitch');
</script>

{#snippet mark(n: number)}
	{#if step > n || step === 3}
		<span class="sn done" aria-label="Done"
			><svg viewBox="0 0 12 12" aria-hidden="true"><path d="m2.5 6.3 2.3 2.3L9.5 3.8" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg></span
		>
	{:else}
		<span class="sn" class:cur={step === n}>{n}</span>
	{/if}
{/snippet}

<Shell>
	<div class="head">
		<h1>{t('nav.home')}</h1>
		{#if step === 3}
			<span class="live">{tn('status.mining', 1, { m: 1 })}</span>
		{:else}
			<span class="live grey">{t('status.not_set_up')}</span>
		{/if}
	</div>

	<section class="panel card" aria-label={t('home.setup_title')}>
		<div class="card-h">
			<h2>{step === 3 ? 'Mining started' : t('home.setup_title')}</h2>
			<span class="mono faint">{done} of 3</span>
		</div>
		<div class="bar" aria-hidden="true"><i style={`width:${(done / 3) * 100}%`}></i></div>

		<ol class="rows">
			<li class:open={step === 1}>
				{@render mark(1)}
				<div class="grow">
					<div class="row-h">
						<h3>{t('home.step1_title')}</h3>
						{#if step > 1}
							<span class="sum"><PlatformMark platform="twitch" />{fake.twitch}</span>
							<a href="#add" class="link">Add another</a>
						{/if}
					</div>
					{#if step === 1}
						<p class="muted">{t('home.step1_body')}</p>
						<div class="seg" role="radiogroup" aria-label="Platform">
							<button type="button" class="btn on" role="radio" aria-checked="true"><PlatformMark platform="twitch" />{t('accounts.twitch')}</button>
							<button type="button" class="btn" role="radio" aria-checked="false"><PlatformMark platform="kick" />{t('accounts.kick')}</button>
						</div>
						<div class="sub">
							<div class="methods">
								<span class="pick on">{t('accounts.method_android')}</span>
								<span class="pick">{t('accounts.method_tv')}</span>
							</div>
							<ol class="how">
								<li>
									<span class="n mono">1</span>
									<div>
										<p>{t('accounts.android_step1')}</p>
										<a class="btn" href="#open">{t('accounts.android_open')}</a>
									</div>
								</li>
								<li>
									<span class="n mono">2</span>
									<div class="grow">
										<p>{t('accounts.android_step2')}</p>
										<div class="paste">
											<div class="field grow">
												<label for={`${uid}-url`}>{t('accounts.android_paste_label')}</label>
												<input id={`${uid}-url`} type="text" placeholder="https://www.twitch.tv/#access_token=…" />
											</div>
											<button type="button" class="btn primary">{t('accounts.android_submit')}</button>
										</div>
									</div>
								</li>
							</ol>
						</div>
					{/if}
				</div>
			</li>

			<li class:open={step === 2} class:locked={step < 2}>
				{@render mark(2)}
				<div class="grow">
					<div class="row-h">
						<h3>{t('home.step2_title')}</h3>
						{#if step > 2}
							<span class="sum ok">{tn('home.games_added', games.length)}</span>
							<a href="/games" class="link">Reorder on Games</a>
						{/if}
					</div>
					{#if step < 2}<p class="faint">After an account is added.</p>{/if}
					{#if step === 2}
						<p class="muted">{t('home.step2_body')}</p>
						<div class="pick-games">
							<label class="search">
								<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg>
								<input type="search" value="Marvel" aria-label={t('home.search_label')} placeholder={t('home.search_ph')} />
							</label>
							<ul class="sugg">
								<li><span class="art" aria-hidden="true">M</span><b class="grow">Marvel Rivals</b><button type="button" class="btn">{t('home.add')}</button></li>
								<li><span class="art" aria-hidden="true">M</span><b class="grow">Marvel Snap</b><button type="button" class="btn">{t('home.add')}</button></li>
							</ul>
							<p class="lbl chosen-h">Chosen</p>
							<ol class="chosen">
								<li><span class="mono faint">1</span><span class="art sm" aria-hidden="true">R</span><b class="grow">Rust</b><span class="faint">Twitch · 4 drops · ends Nov 2</span></li>
							</ol>
						</div>
					{/if}
				</div>
			</li>

			<li class:open={step === 3} class:locked={step < 3}>
				{@render mark(3)}
				<div class="grow">
					<div class="row-h"><h3>Mining starts</h3></div>
					{#if step < 3}
						<p class="faint">Starts by itself once an account and a game are set.</p>
					{:else}
						<div class="run">
							<PlatformMark platform={run.platform} />
							<b class={`pf-name ${run.platform}`}>{run.account}</b>
							<span class="muted grow line">{t('accounts.mining_line', { game: run.game, drop: run.drop, channel: run.channel, m: run.m, req: run.req })}</span>
						</div>
						<div class="bar" aria-hidden="true"><i style={`width:${(run.m / run.req) * 100}%`}></i></div>
						<div class="acts"><button type="button" class="btn">Hide checklist</button></div>
					{/if}
				</div>
			</li>
		</ol>
	</section>
</Shell>

<style>
	.head {
		display: flex;
		align-items: center;
		gap: 16px;
		margin-bottom: 24px;
	}
	.card {
		max-width: 760px;
		padding: 18px 22px 6px;
	}
	.card-h {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		margin-bottom: 10px;
	}
	h2 {
		margin: 0;
		font-size: 15px;
		font-weight: 600;
		letter-spacing: -0.01em;
	}
	.card-h .mono {
		font-size: 12px;
	}
	.rows {
		list-style: none;
		margin: 14px 0 0;
		padding: 0;
	}
	.rows > li {
		display: grid;
		grid-template-columns: 24px minmax(0, 1fr);
		gap: 14px;
		padding: 14px 0;
		border-top: 1px solid var(--border-soft);
	}
	.rows > li.open {
		padding-bottom: 20px;
	}
	.sn {
		width: 24px;
		height: 24px;
		border-radius: 50%;
		display: grid;
		place-items: center;
		font: 500 12px var(--mono);
		color: var(--t3);
		border: 1px solid var(--border);
	}
	.sn.cur {
		color: var(--accent);
		border-color: var(--accent);
		background: var(--accent-soft);
	}
	.sn.done {
		color: var(--green);
		border-color: color-mix(in srgb, var(--green) 50%, transparent);
		background: var(--green-soft);
	}
	.sn svg {
		width: 12px;
		height: 12px;
	}
	.row-h {
		display: flex;
		align-items: center;
		gap: 12px;
		min-height: 24px;
		flex-wrap: wrap;
	}
	h3 {
		margin: 0;
		font-size: 14px;
		font-weight: 600;
	}
	.locked h3 {
		color: var(--t2);
	}
	.sum {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font-size: 13px;
		color: var(--t2);
	}
	.link {
		margin-left: auto;
		font-size: 13px;
		color: var(--accent) !important;
	}
	p {
		margin: 2px 0 0;
		font-size: 13px;
	}
	.seg {
		display: flex;
		gap: 8px;
		margin-top: 14px;
	}
	.seg .on {
		border-color: var(--accent);
		background: var(--accent-soft);
	}
	.sub {
		margin-top: 12px;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--bg);
		padding: 12px 14px 14px;
	}
	.methods {
		display: flex;
		flex-wrap: wrap;
		gap: 4px 16px;
		font-size: 13px;
		padding-bottom: 10px;
		border-bottom: 1px solid var(--border-soft);
	}
	.pick {
		color: var(--t2);
		padding-bottom: 2px;
	}
	.pick.on {
		color: var(--text);
		font-weight: 500;
		box-shadow: inset 0 -2px 0 var(--accent);
	}
	.how {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.how > li {
		display: grid;
		grid-template-columns: 16px minmax(0, 1fr);
		gap: 10px;
		padding-top: 12px;
	}
	.how .n {
		font-size: 12px;
		color: var(--t3);
		padding-top: 2px;
	}
	.how p {
		margin: 0 0 8px;
		max-width: 62ch;
	}
	.paste {
		display: flex;
		align-items: end;
		gap: 8px;
	}
	.paste .btn {
		height: 37px;
	}
	.pick-games {
		margin-top: 12px;
		max-width: 520px;
	}
	.sugg b,
	.chosen b {
		font-weight: 500;
	}
	.chosen-h {
		margin: 16px 0 6px;
	}
	.chosen {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.chosen li {
		display: flex;
		align-items: center;
		gap: 10px;
		font-size: 13px;
		padding: 6px 0;
	}
	.art.sm {
		width: 22px;
		height: 29px;
	}
	.run {
		display: flex;
		align-items: center;
		gap: 10px;
		margin: 8px 0 8px;
		font-size: 13px;
	}
	.line {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.acts {
		margin-top: 14px;
	}
	@container (max-width: 640px) {
		.card {
			padding: 16px 16px 4px;
		}
		.paste {
			flex-direction: column;
			align-items: stretch;
		}
	}
</style>
