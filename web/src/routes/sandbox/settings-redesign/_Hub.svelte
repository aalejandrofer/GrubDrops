<script lang="ts">
	import { tick } from 'svelte';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import Body from './_Body.svelte';
	import Head from './_Head.svelte';
	import Ico from './_Ico.svelte';
	import Top from './_Top.svelte';
	import { GROUPS, SECS, type SecId } from './_data';
	import { reveal, st } from './_state.svelte';

	let cur = $state<SecId | null>(null);
	const sec = $derived(cur ? SECS[cur] : null);
	const group = $derived(sec ? GROUPS.find((g) => g.id === sec.group)! : null);

	async function open(id: SecId | null, key?: string): Promise<void> {
		cur = id;
		await tick();
		if (key) reveal(key);
		else window.scrollTo({ top: 0 });
	}

	/** Cards with a switch right on them: the setting people flip most. */
	const QUICK: Partial<Record<SecId, { label: string; get: () => boolean; flip: () => void }>> = {
		sleep: { label: 'Pause mining every night', get: () => st.sleepOn, flip: () => (st.sleepOn = !st.sleepOn) },
		experimental: { label: 'Shared catalog', get: () => st.share, flip: () => (st.share = !st.share) },
		updates: { label: 'Check automatically', get: () => st.autoCheck, flip: () => (st.autoCheck = !st.autoCheck) }
	};

	const bad = $derived(st.accounts.filter((a) => a.status === 'signed_out'));
</script>

<div class="sx hub">
	{#if !cur}
		<Top />
		{#if bad.length}
			<div class="attn">
				<span class="al"><Ico name="alert" size={15} /> Needs you</span>
				{#each bad as a (a.id)}
					<button type="button" class="pill warn" onclick={() => open('accounts')}>
						<PlatformMark platform={a.platform} /> <span class="pf-name {a.platform}">{a.name}</span> needs login
					</button>
				{/each}
				<button type="button" class="pill bad" onclick={() => open('health', 'canary')}>
					<PlatformMark platform="kick" /> Kick canary failing
				</button>
			</div>
		{/if}

		{#each GROUPS as g (g.id)}
			<section class="group hue-{g.hue}">
				<h2 class="gl">{g.label}</h2>
				<div class="grid" class:wide={g.id === 'accounts'}>
					{#each g.sections as id}
						{@const s = SECS[id]}
						{@const sum = st.summary(id)}
						{@const quick = QUICK[id]}
						<div class="card hue-{s.hue}" class:acc={id === 'accounts'}>
							<span class="tile lg"><Ico name={s.icon} size={19} /></span>
							<div class="ct">
								<button type="button" class="title" onclick={() => open(id)}>{s.label}</button>
								<span class="sum {sum.tone ?? ''}">{sum.text}</span>
								{#if id === 'accounts'}
									<div class="people">
										{#each st.accounts as a (a.id)}
											<span class="who {a.platform}" class:out={a.status === 'signed_out'}>
												<PlatformMark platform={a.platform} />
												{a.name}
											</span>
										{/each}
									</div>
								{/if}
							</div>
							{#if quick}
								<button
									type="button"
									class="sw quick"
									role="switch"
									aria-checked={quick.get()}
									aria-label={quick.label}
									onclick={() => (quick.flip(), st.flash())}
								></button>
							{:else}
								<span class="chev"><Ico name="chevron-right" size={16} /></span>
							{/if}
						</div>
					{/each}
				</div>
			</section>
		{/each}
	{:else if sec && group}
		<div class="crumbs hue-{group.hue}">
			<button type="button" class="back" onclick={() => open(null)}><Ico name="chevron-left" size={15} /> Settings</button>
			<span class="sep">/</span>
			<span class="g">{group.label}</span>
		</div>
		{#if group.sections.length > 1}
			<div class="tabs" role="tablist" aria-label="{group.label} sections">
				{#each group.sections as id}
					{@const s = SECS[id]}
					<button type="button" role="tab" aria-selected={cur === id} class="tab hue-{s.hue}" onclick={() => open(id)}>
						<Ico name={s.icon} size={14} />
						{s.label}
					</button>
				{/each}
			</div>
		{/if}
		<div class="hue-{sec.hue}">
			<Head label={sec.label} icon={sec.icon} hue={sec.hue} blurb={sec.blurb} />
			{#key cur}<Body id={cur} />{/key}
		</div>
	{/if}
</div>

<style>
	.attn {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 8px;
		margin: -6px 0 26px;
	}
	.al {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font-size: 13px;
		font-weight: 600;
		color: var(--amber);
		margin-right: 4px;
	}
	.pill {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 4px 12px 4px 8px;
		border-radius: 999px;
		border: 1px solid;
		font: 500 13px var(--font);
		color: var(--text);
		cursor: pointer;
	}
	.pill.warn {
		border-color: color-mix(in srgb, var(--amber) 50%, transparent);
		background: var(--amber-soft);
	}
	.pill.bad {
		border-color: color-mix(in srgb, var(--red) 50%, transparent);
		background: var(--red-soft);
	}
	.group + .group {
		margin-top: 26px;
	}
	.gl {
		display: flex;
		align-items: center;
		gap: 8px;
		margin: 0 0 10px;
		font-size: 12px;
		font-weight: 600;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--hue);
	}
	.gl::after {
		content: '';
		flex: 1;
		height: 1px;
		background: linear-gradient(to right, color-mix(in srgb, var(--hue) 40%, transparent), transparent);
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(250px, 1fr));
		gap: 12px;
	}
	.card {
		position: relative;
		display: flex;
		align-items: flex-start;
		gap: 12px;
		padding: 16px;
		border: 1px solid var(--border-soft);
		border-radius: 12px;
		background:
			linear-gradient(135deg, color-mix(in srgb, var(--hue) 9%, transparent), transparent 55%),
			var(--surface);
		transition:
			border-color 0.15s,
			transform 0.15s;
	}
	.card:hover {
		border-color: color-mix(in srgb, var(--hue) 55%, transparent);
	}
	.card:has(.title:focus-visible) {
		outline: 2px solid var(--hue);
		outline-offset: 2px;
	}
	.ct {
		flex: 1;
		min-width: 0;
		display: grid;
		gap: 3px;
	}
	.title {
		all: unset;
		font-size: 14.5px;
		font-weight: 600;
		cursor: pointer;
	}
	/* The title button covers the whole card; the switch sits above it. */
	.title::after {
		content: '';
		position: absolute;
		inset: 0;
		border-radius: 12px;
	}
	.sum {
		font-size: 12.5px;
		color: var(--t2);
	}
	.sum.ok {
		color: var(--green);
	}
	.sum.bad {
		color: var(--red);
	}
	.sum.warn {
		color: var(--amber);
	}
	.sum.off {
		color: var(--t3);
	}
	.quick {
		position: relative;
		z-index: 1;
		margin-top: 2px;
	}
	.chev {
		color: var(--t3);
		margin-top: 4px;
	}
	.card:hover .chev {
		color: var(--hue);
	}
	.people {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		margin-top: 8px;
	}
	.who {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 3px 9px 3px 6px;
		border-radius: 999px;
		font-size: 12.5px;
		font-weight: 500;
		border: 1px solid var(--border-soft);
		background: var(--bg);
	}
	.who.twitch {
		color: var(--twitch-fg);
	}
	.who.kick {
		color: var(--kick-fg);
	}
	.who.out {
		border-style: dashed;
		border-color: var(--amber);
		opacity: 0.85;
	}
	.grid.wide {
		grid-template-columns: 1fr;
	}

	.crumbs {
		display: flex;
		align-items: center;
		gap: 8px;
		margin-bottom: 14px;
		font-size: 13.5px;
	}
	.back {
		display: inline-flex;
		align-items: center;
		gap: 3px;
		padding: 4px 6px 4px 2px;
		border: 0;
		border-radius: 6px;
		background: none;
		color: var(--t2);
		font: 500 13.5px var(--font);
		cursor: pointer;
	}
	.back:hover {
		color: var(--text);
		background: var(--surface);
	}
	.sep {
		color: var(--t3);
	}
	.g {
		color: var(--hue);
		font-weight: 600;
	}
	.tabs {
		display: flex;
		gap: 6px;
		margin-bottom: 22px;
		overflow-x: auto;
		scrollbar-width: none;
		padding-bottom: 2px;
	}
	.tab {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		flex: none;
		padding: 6px 12px;
		border-radius: 999px;
		border: 1px solid var(--border-soft);
		background: var(--surface);
		color: var(--t2);
		font: 500 13px var(--font);
		cursor: pointer;
	}
	.tab[aria-selected='true'] {
		color: var(--text);
		border-color: color-mix(in srgb, var(--hue) 55%, transparent);
		background: color-mix(in srgb, var(--hue) 14%, var(--surface));
	}
	.tab[aria-selected='true'] :global(svg) {
		color: var(--hue);
	}

	/* Phone: cards turn into list rows inside one panel per group. */
	@media (max-width: 720px) {
		.attn {
			margin-bottom: 20px;
		}
		.grid {
			grid-template-columns: 1fr;
			gap: 0;
			border: 1px solid var(--border-soft);
			border-radius: 12px;
			background: var(--surface);
			overflow: hidden;
		}
		.card {
			align-items: center;
			padding: 12px 14px;
			border: 0;
			border-radius: 0;
			background: none;
		}
		.card + .card {
			border-top: 1px solid var(--border-soft);
		}
		.card.acc {
			align-items: flex-start;
		}
		.card .tile {
			width: 32px;
			height: 32px;
			border-radius: 9px;
		}
		.title {
			font-size: 14px;
			font-weight: 500;
		}
		.title::after {
			border-radius: 0;
		}
		.quick {
			margin-top: 0;
		}
	}
</style>
