<script lang="ts">
	import { tick } from 'svelte';
	import Hint, { type HintExample } from '$lib/components/Hint.svelte';
	import { ANDROID_EXAMPLE, COOKIES_EXT, KICK_CAT, TWITCH_CAT, type Variant } from './_variants';

	let { variant }: { variant: Variant } = $props();

	const before = $derived(variant.id === 'before');
	const inline = $derived(variant.id === 'b');
	const clickable = $derived(variant.id === 'c');

	let q = $state('');
	let proxy = $state('');
	let gameInput = $state<HTMLInputElement>();
	let proxyInput = $state<HTMLInputElement>();

	const gameEgs: HintExample[] = [
		{ platform: 'twitch', label: 'twitch.tv/directory/category/rust', value: TWITCH_CAT },
		{ platform: 'kick', label: 'kick.com/category/rust', value: KICK_CAT }
	];
	const proxyEgs: HintExample[] = [{ label: 'http://' }, { label: 'https://' }, { label: 'socks5://' }];
	const androidEgs: HintExample[] = [{ platform: 'twitch', label: ANDROID_EXAMPLE }];

	/** Fills the search with the link and selects the slug, so typing a game replaces it. */
	async function pickGame(v: string): Promise<void> {
		q = v;
		await tick();
		gameInput?.focus();
		gameInput?.setSelectionRange(v.lastIndexOf('/') + 1, v.length);
	}

	async function pickProxy(v: string): Promise<void> {
		proxy = v;
		await tick();
		proxyInput?.focus();
		proxyInput?.setSelectionRange(v.length, v.length);
	}
</script>

{#snippet cookie()}
	<svg class="cookie" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M21.95 12.9A10 10 0 1 1 11.1 2.05a3 3 0 0 0 3.4 3.95 3 3 0 0 0 3.5 3.5 3 3 0 0 0 3.95 3.4z" />
		<circle cx="8" cy="9" r="1.5" />
		<circle cx="15.5" cy="15" r="1.5" />
		<circle cx="9" cy="16" r="1.2" />
		<circle cx="13" cy="11.5" r="1" />
	</svg>
{/snippet}

<div class="frame">
	<p class="where">Games, Add a game</p>
	<section class="add" aria-label="Add a game">
		<label class="search">
			<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg>
			<input type="search" bind:value={q} bind:this={gameInput} placeholder="Game name or category link" aria-label="Add a game" autocomplete="off" />
		</label>
		{#if before}
			<p class="eg">e.g. https://www.twitch.tv/directory/category/rust</p>
			<details class="help">
				<summary>Campaign missing?</summary>
				<p>
					Names must match each platform exactly. Twitch's World of Warcraft is World of Warcraft: Forever on Kick. Add both names, or paste
					the category link (kick.com/category/…).
				</p>
			</details>
		{:else}
			{#if clickable}
				<Hint examples={gameEgs} onpick={pickGame} />
			{:else}
				<Hint text="Names differ per platform? Paste each link." examples={gameEgs} {inline} />
			{/if}
		{/if}
	</section>
	<div class="ghost" aria-hidden="true">
		<span></span><span></span>
	</div>
</div>

<p class="where out">{before ? "Other field hints today" : "Same component in other spots"}</p>
<div class="spots">
	<div class="panel">
		<p class="where">Points, Twitch tab</p>
		<span class="rh">Add a Twitch channel</span>
		<span class="row">
			<input class="inp" type="url" placeholder="https://www.twitch.tv/keshaeuw" />
			<button type="button" class="btn primary" disabled>Add</button>
		</span>
		{#if before}
			<p class="eg2">Added last, no accounts ticked.</p>
		{:else if !clickable}
			<Hint text="Added last, no accounts ticked." />
		{/if}
	</div>

	<div class="panel">
		<p class="where">Settings, Engine, Proxy</p>
		<label class="lbl" for="px-{variant.id}">Proxy URL</label>
		<span class="row">
			<input
				class="inp mono"
				id="px-{variant.id}"
				type="text"
				spellcheck="false"
				autocomplete="off"
				placeholder="socks5://user:pass@127.0.0.1:1080"
				bind:value={proxy}
				bind:this={proxyInput}
			/>
			<button type="button" class="btn" disabled>Test</button>
		</span>
		{#if before}
			<small class="help2">http://, https:// or socks5://. Add user:pass@ if needed.</small>
		{:else}
			<Hint text="Add user:pass@ if needed." examples={proxyEgs} {inline} onpick={clickable ? pickProxy : undefined} />
		{/if}
	</div>

	<div class="panel">
		<p class="where">Add account, Kick</p>
		<label class="lbl" for="ck-{variant.id}">cookies.txt</label>
		<textarea class="inp ta" id="ck-{variant.id}" rows="3" spellcheck="false"></textarea>
		{#if before}
			<small class="help2">Export cookies for kick.com in Netscape format.</small>
		{:else if clickable}
			<Hint text="Export cookies for kick.com in Netscape format." examples={[{ label: 'Get cookies.txt', href: COOKIES_EXT, icon: cookie }]} />
		{:else}
			<Hint text="Export cookies for kick.com in Netscape format." />
		{/if}
	</div>

	<div class="panel">
		<p class="where">Add account, Twitch Android login</p>
		<label class="lbl" for="an-{variant.id}">Twitch homepage URL</label>
		<input class="inp" id="an-{variant.id}" type="text" spellcheck="false" autocomplete="off" />
		{#if before}
			<small class="help2">Looks like: <code>{ANDROID_EXAMPLE}</code></small>
		{:else}
			<Hint examples={androidEgs} {inline} />
		{/if}
	</div>
</div>

<style>
	.frame {
		max-width: 760px;
		padding: 20px;
		border: 1px solid var(--border-soft);
		border-radius: 12px;
		background: var(--bg);
		color: var(--text);
		font-size: 13.5px;
	}
	.where {
		margin: 0 0 10px;
		font-size: 11px;
		color: #8a8a92;
	}
	.where.out {
		margin: 20px 0 8px;
	}
	.i {
		width: 16px;
		height: 16px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.8;
		stroke-linecap: round;
		stroke-linejoin: round;
		flex: none;
	}
	.add {
		position: relative;
		margin-bottom: 14px;
	}
	.search {
		display: flex;
		align-items: center;
		gap: 10px;
		height: 42px;
		padding: 0 14px;
		border: 1px solid var(--border);
		border-radius: 10px;
		background: var(--surface);
		color: var(--t3);
	}
	.search:focus-within {
		border-color: var(--accent);
		box-shadow: 0 0 0 3px var(--accent-soft);
	}
	.search input {
		flex: 1;
		min-width: 0;
		background: none;
		border: 0;
		outline: 0;
		color: var(--text);
		font: inherit;
	}
	.search input::placeholder {
		color: var(--t3);
	}
	.search input::selection {
		background: var(--accent-soft);
		color: var(--text);
	}
	.eg {
		margin: 6px 2px 0;
		font-size: 11.5px;
		color: var(--t3);
	}
	.help {
		margin: 6px 2px 0;
		font-size: 12px;
		color: var(--t3);
	}
	.help summary {
		cursor: pointer;
		width: fit-content;
	}
	.help summary:hover {
		color: var(--t2);
	}
	.help p {
		margin: 4px 0 0;
		max-width: 60ch;
		line-height: 1.45;
		color: var(--t2);
	}
	.cookie path {
		fill: var(--h-amber);
	}
	.cookie circle {
		fill: color-mix(in srgb, var(--h-amber) 35%, #000);
	}
	.ghost {
		display: grid;
		gap: 8px;
	}
	.ghost span {
		height: 64px;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
	}
	.spots {
		max-width: 760px;
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 12px;
	}
	.panel {
		display: grid;
		align-content: start;
		gap: 6px;
		min-width: 0;
		padding: 14px 16px;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		color: var(--text);
		font-size: 13px;
	}
	.panel .where {
		margin-bottom: 4px;
	}
	.rh {
		font-size: 11px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--t3);
	}
	.lbl {
		font-size: 12.5px;
		font-weight: 500;
		color: var(--t2);
	}
	.row {
		display: flex;
		gap: 8px;
		min-width: 0;
	}
	.inp {
		flex: 1;
		min-width: 0;
		width: 100%;
		height: 32px;
		padding: 0 10px;
		border: 1px solid var(--border);
		border-radius: 7px;
		background: var(--bg);
		color: var(--text);
		font: inherit;
		font-size: 12.5px;
	}
	.inp.mono {
		font: 12.5px var(--mono);
	}
	.inp:focus {
		outline: 0;
		border-color: var(--accent);
		box-shadow: 0 0 0 3px var(--accent-soft);
	}
	.inp::placeholder {
		color: var(--t3);
	}
	.ta {
		height: auto;
		padding: 8px 10px;
		resize: vertical;
	}
	.btn {
		flex: none;
		height: 32px;
		padding: 0 12px;
		border: 1px solid var(--border);
		border-radius: 7px;
		background: var(--surface-2);
		color: var(--text);
		font: inherit;
		font-size: 12.5px;
	}
	.btn.primary {
		background: var(--accent-strong);
		border-color: var(--accent-strong);
		color: #fff;
	}
	.btn:disabled {
		opacity: 0.55;
	}
	.eg2 {
		margin: 0;
		font-size: 12px;
		color: var(--dim);
	}
	.help2 {
		color: var(--t2);
		font-size: 12px;
		overflow-wrap: anywhere;
	}
	.help2 code {
		font-family: var(--mono);
	}
	@media (max-width: 640px) {
		.frame {
			padding: 0;
			border: 0;
			background: none;
		}
		.spots {
			grid-template-columns: minmax(0, 1fr);
		}
	}
</style>
