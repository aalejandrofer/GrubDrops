<script lang="ts">
	import { onMount } from 'svelte';
	import Body from './_Body.svelte';
	import Head from './_Head.svelte';
	import Ico from './_Ico.svelte';
	import Top from './_Top.svelte';
	import { GROUPS, ORDER, SECS, type SecId } from './_data';
	import { st } from './_state.svelte';

	let active = $state<SecId>('accounts');
	let sheet = $state(false);
	let sheetEl: HTMLElement | undefined = $state();
	let jumpEl: HTMLElement | undefined = $state();
	let hold = 0;

	onMount(() => {
		st.barMode = true;
		// One pending edit so the bar shows on load.
		st.set('sleepStart', '00:30');
		return () => {
			st.discard();
			st.barMode = false;
		};
	});

	function track(): void {
		if (Date.now() < hold) return;
		if (window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 2) {
			active = ORDER[ORDER.length - 1];
			return;
		}
		let cur: SecId = ORDER[0];
		for (const id of ORDER) {
			const el = document.getElementById(`ss-${id}`);
			if (el && el.getBoundingClientRect().top <= 150) cur = id;
		}
		active = cur;
	}
	function jump(id: SecId): void {
		active = id;
		sheet = false;
		hold = Date.now() + 900;
		document.getElementById(`ss-${id}`)?.scrollIntoView({ behavior: 'smooth' });
	}
	function outside(e: PointerEvent): void {
		if (!sheet) return;
		const t = e.target as Node;
		if (sheetEl?.contains(t) || jumpEl?.contains(t)) return;
		sheet = false;
	}
	const sec = $derived(SECS[active]);
</script>

<svelte:window onscroll={track} onpointerdown={outside} onkeydown={(e) => e.key === 'Escape' && (sheet = false)} />

<div class="sx scroll">
	<Top stamp="Switches save at once. Typed fields wait for Save." />

	<div class="jumpbar hue-{sec.hue}">
		<button
			type="button"
			class="jump"
			bind:this={jumpEl}
			aria-expanded={sheet}
			aria-controls="ss-sheet"
			onclick={() => (sheet = !sheet)}
		>
			<span class="tile sm"><Ico name={sec.icon} size={13} /></span>
			<span class="jl">{sec.label}</span>
			<span class="jn">{ORDER.indexOf(active) + 1} / {ORDER.length}</span>
			<Ico name="chevron-down" size={15} />
		</button>
		{#if sheet}
			<div class="sheet" id="ss-sheet" bind:this={sheetEl}>
				{#each GROUPS as g (g.id)}
					<div class="sg hue-{g.hue}">{g.label}</div>
					{#each g.sections as id}
						{@const s = SECS[id]}
						<button type="button" class="sl hue-{s.hue}" class:on={active === id} onclick={() => jump(id)}>
							<span class="tile sm"><Ico name={s.icon} size={13} /></span>
							{s.label}
						</button>
					{/each}
				{/each}
			</div>
		{/if}
	</div>

	<div class="layout">
		<nav class="index" aria-label="Settings sections">
			{#each GROUPS as g (g.id)}
				<div class="ig hue-{g.hue}">
					<span class="gl">{g.label}</span>
					{#each g.sections as id}
						{@const s = SECS[id]}
						<a
							href="#ss-{id}"
							class="navlink hue-{s.hue}"
							class:on={active === id}
							aria-current={active === id ? 'true' : undefined}
							onclick={(e) => (e.preventDefault(), jump(id))}
						>
							<span class="tile sm"><Ico name={s.icon} size={12} /></span>
							{s.label}
							{#if st.summary(id).tone === 'bad' || st.summary(id).tone === 'warn'}
								<span class="dot {st.summary(id).tone}" aria-hidden="true"></span>
							{/if}
						</a>
					{/each}
				</div>
			{/each}
		</nav>

		<div class="bands">
			{#each GROUPS as g (g.id)}
				<div class="band hue-{g.hue}">
					{#if g.sections.length > 1}
						<h2 class="bh"><span class="tile"><Ico name={g.icon} size={15} /></span>{g.label}</h2>
					{/if}
					{#each g.sections as id}
						{@const s = SECS[id]}
						<section class="card hue-{s.hue}" id="ss-{id}">
							<Head label={s.label} icon={s.icon} hue={s.hue} blurb={s.blurb} />
							<Body {id} />
						</section>
					{/each}
				</div>
			{/each}
		</div>
	</div>

	{#if st.dirtyLabels.length}
		<div class="bar" role="region" aria-label="Unsaved changes">
			<span class="bd" aria-hidden="true"></span>
			<span class="bt">
				<b>{st.dirtyLabels.length} unsaved {st.dirtyLabels.length === 1 ? 'change' : 'changes'}</b>
				<small>{st.dirtyLabels.join(', ')}</small>
			</span>
			<button type="button" class="btn ghost" onclick={() => st.discard()}>Discard</button>
			<button type="button" class="btn primary" onclick={() => st.saveAll()}>Save</button>
		</div>
	{/if}
</div>

<style>
	.scroll {
		padding-bottom: 80px;
	}
	.layout {
		display: grid;
		grid-template-columns: 210px minmax(0, 1fr);
		gap: 36px;
		align-items: start;
	}
	.index {
		position: sticky;
		top: 16px;
		display: grid;
		gap: 14px;
	}
	.gl {
		display: block;
		padding: 0 8px 3px;
		font-size: 11.5px;
		font-weight: 600;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--hue);
	}
	.navlink {
		display: flex;
		align-items: center;
		gap: 9px;
		padding: 4px 8px;
		border-radius: 7px;
		color: var(--t2);
		font-size: 13px;
		font-weight: 500;
	}
	.navlink:hover {
		color: var(--text);
		background: var(--surface);
	}
	.navlink.on {
		color: var(--text);
		background: color-mix(in srgb, var(--hue) 13%, transparent);
	}
	.navlink .tile {
		width: 20px;
		height: 20px;
	}
	.dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		margin-left: auto;
	}
	.dot.warn {
		background: var(--amber);
	}
	.dot.bad {
		background: var(--red);
	}
	.band + .band {
		margin-top: 34px;
	}
	.bh {
		display: flex;
		align-items: center;
		gap: 10px;
		margin: 0 0 12px;
		font-size: 18px;
		font-weight: 650;
		letter-spacing: -0.015em;
		color: var(--hue);
	}
	.card {
		scroll-margin-top: 40px;
	}
	.card + .card {
		margin-top: 28px;
	}
	.jumpbar {
		display: none;
	}
	.bar {
		position: fixed;
		left: 50%;
		bottom: 18px;
		transform: translateX(-50%);
		z-index: 30;
		width: min(640px, calc(100% - 32px));
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 10px 10px 10px 16px;
		border: 1px solid color-mix(in srgb, var(--amber) 50%, var(--border));
		border-radius: 12px;
		background: var(--surface-2);
		box-shadow: 0 10px 30px rgba(0, 0, 0, 0.35);
	}
	.bd {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--amber);
		flex: none;
	}
	.bt {
		flex: 1;
		min-width: 0;
		display: grid;
	}
	.bt b {
		font-size: 13.5px;
		font-weight: 600;
	}
	.bt small {
		color: var(--t2);
		font-size: 12px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	@media (max-width: 720px) {
		.layout {
			grid-template-columns: minmax(0, 1fr);
		}
		.index {
			display: none;
		}
		.jumpbar {
			display: block;
			position: sticky;
			top: 0;
			z-index: 20;
			margin: 0 -16px 16px;
			padding: 8px 16px;
			background: color-mix(in srgb, var(--bg) 94%, transparent);
			border-bottom: 1px solid var(--border-soft);
		}
		.jump {
			display: flex;
			align-items: center;
			gap: 10px;
			width: 100%;
			padding: 6px 10px 6px 6px;
			border: 1px solid color-mix(in srgb, var(--hue) 45%, var(--border));
			border-radius: 10px;
			background: color-mix(in srgb, var(--hue) 9%, var(--surface));
			color: var(--text);
			font: 500 14px var(--font);
			cursor: pointer;
		}
		.jl {
			flex: 1;
			text-align: left;
		}
		.jn {
			font: 12px var(--mono);
			color: var(--t3);
		}
		.sheet {
			position: absolute;
			left: 16px;
			right: 16px;
			top: calc(100% + 4px);
			max-height: 70vh;
			overflow-y: auto;
			padding: 8px;
			border: 1px solid var(--border);
			border-radius: 12px;
			background: var(--surface-2);
			box-shadow: 0 16px 40px rgba(0, 0, 0, 0.4);
		}
		.sg {
			padding: 8px 8px 4px;
			font-size: 11.5px;
			font-weight: 600;
			letter-spacing: 0.06em;
			text-transform: uppercase;
			color: var(--hue);
		}
		.sl {
			display: flex;
			align-items: center;
			gap: 10px;
			width: 100%;
			padding: 8px;
			border: 0;
			border-radius: 8px;
			background: none;
			color: var(--text);
			font: 500 14px var(--font);
			text-align: left;
			cursor: pointer;
		}
		.sl.on {
			background: color-mix(in srgb, var(--hue) 14%, transparent);
		}
		.card {
			scroll-margin-top: 94px;
			padding: 14px 12px 16px;
		}
		.bar {
			bottom: 0;
			width: 100%;
			border-radius: 12px 12px 0 0;
			border-bottom: 0;
			padding: 10px 12px 14px 14px;
		}
	}
</style>
