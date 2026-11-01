<script lang="ts">
	import { tick } from 'svelte';
	import Body from './_Body.svelte';
	import Head from './_Head.svelte';
	import Ico from './_Ico.svelte';
	import Top from './_Top.svelte';
	import { GROUPS, SECS, type SecId } from './_data';
	import { reveal, search, st } from './_state.svelte';

	let cur = $state<SecId>('accounts');
	/** Phone only: the list of sections, or one open section. */
	let phonePage = $state(false);
	let q = $state('');
	let box: HTMLInputElement | undefined = $state();
	const hits = $derived(search(q));
	const sec = $derived(SECS[cur]);
	const group = $derived(GROUPS.find((g) => g.id === sec.group)!);

	async function open(id: SecId, key?: string): Promise<void> {
		cur = id;
		phonePage = true;
		q = '';
		await tick();
		if (key) reveal(key);
		else window.scrollTo({ top: 0 });
	}

	function onKey(e: KeyboardEvent): void {
		const t = e.target as HTMLElement | null;
		if (e.key === '/' && t?.tagName !== 'INPUT' && t?.tagName !== 'TEXTAREA') {
			e.preventDefault();
			box?.focus();
		}
	}

	/** Highlights the typed text inside a result label. */
	function parts(label: string): [string, string, string] {
		const i = label.toLowerCase().indexOf(q.trim().toLowerCase());
		if (i < 0 || !q.trim()) return [label, '', ''];
		const n = q.trim().length;
		return [label.slice(0, i), label.slice(i, i + n), label.slice(i + n)];
	}
</script>

<svelte:window onkeydown={onKey} />

<div class="sx sidebar" class:page={phonePage}>
	<Top />
	<div class="layout">
		<aside class="nav">
			<div class="search hue-violet">
				<Ico name="search" size={15} />
				<input
					bind:this={box}
					bind:value={q}
					type="search"
					placeholder="Search settings"
					aria-label="Search settings"
					onkeydown={(e) => {
						if (e.key === 'Escape') q = '';
						if (e.key === 'Enter' && hits[0]) open(hits[0].sec, hits[0].key);
					}}
				/>
				<kbd>/</kbd>
			</div>

			{#if q.trim()}
				<ul class="hits" aria-label="Search results">
					{#each hits as h (h.key)}
						{@const s = SECS[h.sec]}
						{@const p = parts(h.label)}
						<li>
							<button type="button" class="hit hue-{s.hue}" onclick={() => open(h.sec, h.key)}>
								<span class="tile sm"><Ico name={s.icon} size={13} /></span>
								<span class="ht">
									<span>{p[0]}<mark>{p[1]}</mark>{p[2]}</span>
									<small>{s.label}</small>
								</span>
							</button>
						</li>
					{:else}
						<li class="none">No setting matches “{q}”.</li>
					{/each}
				</ul>
			{:else}
				{#each GROUPS as g (g.id)}
					<div class="group hue-{g.hue}">
						<div class="gl">{g.label}</div>
						{#each g.sections as id}
							{@const s = SECS[id]}
							{@const sum = st.summary(id)}
							<button
								type="button"
								class="navlink hue-{s.hue}"
								class:on={cur === id}
								aria-current={cur === id ? 'page' : undefined}
								onclick={() => open(id)}
							>
								<span class="tile sm"><Ico name={s.icon} size={13} /></span>
								<span class="nt">
									<span class="nm">{s.label}</span>
									<small class="sum {sum.tone ?? ''}">{sum.text}</small>
								</span>
								{#if sum.tone === 'bad' || sum.tone === 'warn'}<span class="dot {sum.tone}" aria-hidden="true"></span>{/if}
								<span class="chev"><Ico name="chevron-right" size={15} /></span>
							</button>
						{/each}
					</div>
				{/each}
			{/if}
		</aside>

		<main class="content hue-{sec.hue}">
			<button type="button" class="back" onclick={() => (phonePage = false)}>
				<Ico name="chevron-left" size={15} /> Settings
			</button>
			<p class="crumb hue-{group.hue}">{group.label}</p>
			<Head label={sec.label} icon={sec.icon} hue={sec.hue} blurb={sec.blurb} />
			{#key cur}<Body id={cur} />{/key}
		</main>
	</div>
</div>

<style>
	.layout {
		display: grid;
		grid-template-columns: 250px minmax(0, 1fr);
		gap: 36px;
		align-items: start;
	}
	.nav {
		position: sticky;
		top: 16px;
		max-height: calc(100vh - 32px);
		overflow-y: auto;
		scrollbar-width: thin;
		padding-right: 4px;
	}
	.search {
		display: flex;
		align-items: center;
		gap: 8px;
		height: 36px;
		padding: 0 8px 0 10px;
		border: 1px solid var(--border);
		border-radius: 9px;
		background: var(--surface);
		color: var(--t3);
		margin-bottom: 14px;
	}
	.search:focus-within {
		border-color: var(--hue);
		box-shadow: 0 0 0 3px color-mix(in srgb, var(--hue) 22%, transparent);
	}
	.search input {
		flex: 1;
		min-width: 0;
		border: 0;
		background: none;
		color: var(--text);
		font: 13.5px var(--font);
		outline: 0;
	}
	.search input::placeholder {
		color: var(--t3);
	}
	kbd {
		font: 11px var(--mono);
		color: var(--t3);
		border: 1px solid var(--border);
		border-radius: 4px;
		padding: 0 5px;
	}
	.group + .group {
		margin-top: 14px;
	}
	.gl {
		display: flex;
		align-items: center;
		gap: 7px;
		padding: 0 8px 4px;
		font-size: 11.5px;
		font-weight: 600;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--hue);
	}
	.gl::before {
		content: '';
		width: 6px;
		height: 6px;
		border-radius: 2px;
		background: var(--hue);
	}
	.navlink,
	.hit {
		display: flex;
		align-items: center;
		gap: 10px;
		width: 100%;
		padding: 6px 8px;
		border: 0;
		border-radius: 8px;
		background: none;
		color: var(--t2);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.navlink:hover,
	.hit:hover {
		background: var(--surface);
		color: var(--text);
	}
	.navlink.on {
		background: color-mix(in srgb, var(--hue) 13%, transparent);
		color: var(--text);
	}
	.nt,
	.ht {
		flex: 1;
		min-width: 0;
		display: grid;
	}
	.nm,
	.ht > span {
		font-size: 13.5px;
		font-weight: 500;
	}
	.sum,
	.ht small {
		display: none;
		font-size: 12px;
		color: var(--t3);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.ht small {
		display: block;
		color: var(--hue);
	}
	.sum.bad {
		color: var(--red);
	}
	.sum.warn {
		color: var(--amber);
	}
	.dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		flex: none;
	}
	.dot.warn {
		background: var(--amber);
	}
	.dot.bad {
		background: var(--red);
	}
	.chev {
		display: none;
		color: var(--t3);
	}
	.hits {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 2px;
	}
	.none {
		padding: 8px;
		color: var(--t3);
		font-size: 13px;
	}
	.content {
		min-width: 0;
	}
	.back {
		display: none;
	}
	.crumb {
		margin: 0 0 6px;
		font-size: 11.5px;
		font-weight: 600;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--hue);
	}

	/* Phone: the nav becomes the Settings home list; a section opens as its own page with a back link. */
	@media (max-width: 720px) {
		.layout {
			grid-template-columns: minmax(0, 1fr);
			gap: 0;
		}
		.nav {
			position: static;
			max-height: none;
			overflow: visible;
			padding: 0;
		}
		.page .nav,
		.sidebar:not(.page) .content {
			display: none;
		}
		.group {
			border: 1px solid var(--border-soft);
			border-radius: 12px;
			background: var(--surface);
			padding: 8px 0 4px;
		}
		.gl {
			padding: 2px 14px 6px;
		}
		.navlink {
			padding: 10px 14px;
			border-radius: 0;
			border-top: 1px solid var(--border-soft);
		}
		.navlink.on {
			background: none;
			box-shadow: none;
			color: var(--t2);
		}
		.navlink .tile {
			width: 30px;
			height: 30px;
			border-radius: 8px;
		}
		.nm {
			color: var(--text);
		}
		.sum {
			display: block;
		}
		.chev {
			display: block;
		}
		kbd {
			display: none;
		}
		.back {
			display: inline-flex;
			align-items: center;
			gap: 4px;
			margin: 0 0 14px -4px;
			padding: 4px;
			border: 0;
			background: none;
			color: var(--hue);
			font: 500 14px var(--font);
			cursor: pointer;
		}
	}
</style>
