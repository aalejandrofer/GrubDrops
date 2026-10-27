<script lang="ts">
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { accounts, channels as seed, parseChannel, type Chan, type Variant } from './_variants';

	let { variant }: { variant: Variant } = $props();

	let chans = $state<Chan[]>(structuredClone(seed));
	/** null = closed; 'new' = adding; else the channel id being edited */
	let form = $state<string | null>(null);
	let url = $state('');
	let picked = $state<string[]>([]);

	const parsed = $derived(form === 'new' ? parseChannel(url) : null);
	const editing = $derived(form && form !== 'new' ? chans.find((c) => c.id === form) : undefined);
	const plat = $derived(editing?.platform ?? parsed?.platform);
	const choices = $derived(plat ? accounts.filter((a) => a.platform === plat) : []);
	const dupe = $derived(parsed && chans.some((c) => c.platform === parsed.platform && c.name === parsed.name));
	const urlErr = $derived(form === 'new' && url.trim() && !parsed ? 'Paste a twitch.tv or kick.com channel link.' : dupe ? 'Already in the list.' : '');
	const canSave = $derived(picked.length > 0 && (editing || (parsed && !dupe)));

	const name = (id: string) => {
		const a = accounts.find((x) => x.id === id)!;
		return a.name;
	};
	/** Static stand-in for the runner: the first live channel an idle account is on. */
	const watching = $derived(
		Object.fromEntries(
			accounts.map((a) => [a.id, chans.find((c) => c.live && c.accounts.includes(a.id))?.id ?? null])
		) as Record<string, string | null>
	);

	function open(id: string | null) {
		form = id ?? 'new';
		url = '';
		picked = id ? [...chans.find((c) => c.id === id)!.accounts] : [];
	}
	function close() {
		form = null;
	}
	function save() {
		if (!canSave) return;
		if (editing) editing.accounts = [...picked];
		else if (parsed) chans.push({ id: `n${Date.now()}`, ...parsed, live: false, accounts: [...picked] });
		close();
	}
	function move(i: number, d: number) {
		const j = i + d;
		if (j < 0 || j >= chans.length) return;
		[chans[i], chans[j]] = [chans[j], chans[i]];
	}
	let confirming = $state<string | null>(null);
	function remove(id: string) {
		chans = chans.filter((x) => x.id !== id);
		confirming = null;
	}
	function pick(id: string) {
		picked = picked.includes(id) ? picked.filter((x) => x !== id) : [...picked, id];
	}
</script>

{#snippet fields()}
	{#if form === 'new'}
		<label class="fl">
			<span>Channel link (twitch.tv / kick.com)</span>
			<!-- svelte-ignore a11y_autofocus -->
			<input type="url" placeholder="https://www.twitch.tv/keshaeuw" bind:value={url} autofocus />
		</label>
		{#if urlErr}<p class="bad">{urlErr}</p>{/if}
		{#if parsed && !dupe}
			<p class="found"><PlatformMark platform={parsed.platform} /><b>{parsed.name}</b><span>{parsed.platform === 'twitch' ? 'Twitch' : 'Kick'} channel</span></p>
		{/if}
	{/if}
	{#if plat}
		<fieldset>
			<legend>Accounts that watch it when idle</legend>
			{#if choices.length}
				<div class="chips">
					{#each choices as a (a.id)}
						<button type="button" class="chip" class:on={picked.includes(a.id)} aria-pressed={picked.includes(a.id)} onclick={() => pick(a.id)}
							><PlatformMark platform={a.platform} size="sm" />{a.name}</button
						>
					{/each}
				</div>
			{:else}
				<p class="bad">No {plat === 'twitch' ? 'Twitch' : 'Kick'} accounts yet.</p>
			{/if}
		</fieldset>
	{/if}
	<div class="acts">
		<button type="button" class="btn" onclick={close}>Cancel</button>
		<button type="button" class="btn primary" disabled={!canSave} onclick={save}>{editing ? 'Save' : 'Add'}</button>
	</div>
{/snippet}

<div class="frame">
	<div class="cols">
		<div class="side">
			<aside class="rail">
				<h2 class="rh">Lists</h2>
				<div class="ri"><span class="mk">≡</span><span class="who"><b>Shared list</b><small>28 games · 4 accounts</small></span></div>
				<h3 class="rh">Accounts</h3>
				{#each accounts as a (a.id)}
					<div class="ri"><PlatformMark platform={a.platform} /><span class="who"><b>{a.name}</b><small>Shared list</small></span><span class="sw"></span></div>
				{/each}
			</aside>

			<aside class="rail fw" aria-labelledby={`fw-${variant.id}`}>
				<div class="head">
					<h2 class="rh" id={`fw-${variant.id}`}>Force watch</h2>
					<button type="button" class="plus" aria-label="Add channel" title="Add channel" onclick={() => open(null)}>+</button>
				</div>
				<p class="hint">Watched when no drops.</p>
				{#if chans.length === 0}
					<p class="empty">No channels yet.</p>
				{/if}
				<ol>
					{#each chans as c, i (c.id)}
						<li class:editing={variant.id === 'b' && form === c.id}>
							<div class="row">
								<span class="rk">{i + 1}</span>
								<PlatformMark platform={c.platform} />
								<span class="who">
									<b>{c.name}<span class="dot" class:live={c.live} title={c.live ? 'Live' : 'Offline'}></span></b>
									<small>
										{#each c.accounts as id, k (id)}{k ? ' · ' : ''}<span class:now={watching[id] === c.id}>{name(id)}</span>{/each}
									</small>
								</span>
								<span class="tools">
									<button type="button" class="ico" aria-label="Move up" disabled={i === 0} onclick={() => move(i, -1)}>↑</button>
									<button type="button" class="ico" aria-label="Move down" disabled={i === chans.length - 1} onclick={() => move(i, 1)}>↓</button>
									<button type="button" class="ico" aria-label="Edit accounts" onclick={() => open(c.id)}>✎</button>
									<button type="button" class="ico" aria-label="Remove" onclick={() => (confirming = c.id)}>×</button>
								</span>
							</div>
							{#if confirming === c.id}
								<div class="conf">
									<span>Remove {c.name}?</span>
									<button type="button" class="danger" onclick={() => remove(c.id)}>Remove</button>
									<button type="button" class="plain" onclick={() => (confirming = null)}>Cancel</button>
								</div>
							{/if}
							{#if variant.id === 'b' && form === c.id}
								<div class="inl">{@render fields()}</div>
							{/if}
						</li>
					{/each}
				</ol>
				{#if variant.id === 'b' && form === 'new'}
					<div class="inl">{@render fields()}</div>
				{/if}
			</aside>
		</div>

		<div class="main" aria-hidden="true">
			<div class="ph"></div>
			<div class="ph tall"></div>
			<div class="ph tall"></div>
		</div>
	</div>

	{#if variant.id === 'a' && form}
		<div class="scrim" role="presentation" onclick={(e) => e.target === e.currentTarget && close()}>
			<div class="dlg" role="dialog" aria-modal="true" aria-label={editing ? `Edit ${editing.name}` : 'Add channel'}>
				<h2>{editing ? editing.name : 'Add channel'}</h2>
				{@render fields()}
			</div>
		</div>
	{/if}
</div>

<style>
	.frame {
		position: relative;
		padding: 20px;
		border: 1px solid var(--border);
		border-radius: 12px;
		background: var(--bg);
		color: var(--text);
		overflow: hidden;
		min-height: 640px;
	}
	.cols {
		display: grid;
		grid-template-columns: 250px minmax(0, 1fr);
		gap: 22px;
		align-items: start;
	}
	.side {
		display: grid;
		gap: 12px;
	}
	.rail {
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		padding: 8px;
	}
	.rh {
		margin: 8px 8px 4px;
		font-size: 11px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--t3);
	}
	.ri,
	.row {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 8px;
	}
	.mk {
		width: 14px;
		color: var(--accent);
		text-align: center;
	}
	.who {
		display: grid;
		min-width: 0;
		flex: 1;
	}
	.who b {
		font-size: 13px;
		display: flex;
		align-items: center;
		gap: 6px;
	}
	.who small {
		color: var(--t3);
		font-size: 12px;
	}
	.sw {
		width: 30px;
		height: 16px;
		border-radius: 999px;
		background: var(--surface-3);
		flex: none;
	}
	.hint {
		margin: 0 8px 6px;
		font-size: 12px;
		color: var(--t3);
	}
	.empty {
		margin: 6px 8px;
		font-size: 12px;
		color: var(--dim);
	}
	ol {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	li {
		border-radius: 8px;
	}
	li:hover,
	li.editing {
		background: var(--surface-2);
	}
	.rk {
		width: 12px;
		font-family: var(--mono);
		font-size: 11px;
		color: var(--dim);
	}
	.dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--surface-3);
	}
	.dot.live {
		background: var(--red);
	}
	.now {
		color: var(--green);
		font-weight: 600;
	}
	.tools {
		display: flex;
		opacity: 0.35;
	}
	li:hover .tools,
	li:focus-within .tools {
		opacity: 1;
	}
	.ico {
		border: 0;
		background: none;
		color: var(--t2);
		width: 18px;
		padding: 0;
		cursor: pointer;
		font-size: 13px;
	}
	.ico[disabled] {
		opacity: 0.3;
		cursor: default;
	}
	.head {
		display: flex;
		align-items: center;
		justify-content: space-between;
	}
	.plus {
		margin: 4px 4px 0 0;
		width: 24px;
		height: 24px;
		border: 0;
		border-radius: 6px;
		background: none;
		color: var(--accent);
		font-size: 18px;
		line-height: 1;
		cursor: pointer;
	}
	.plus:hover {
		background: var(--accent-soft);
	}
	.conf {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px;
		padding: 0 8px 8px 30px;
		font-size: 12px;
		color: var(--t2);
	}
	.conf span {
		flex-basis: 100%;
	}
	.conf button {
		font: inherit;
		font-size: 12px;
		padding: 3px 9px;
		border-radius: 6px;
		cursor: pointer;
	}
	.conf .danger {
		border: 1px solid var(--red);
		background: var(--red-soft);
		color: var(--red);
	}
	.conf .plain {
		border: 1px solid var(--border);
		background: none;
		color: var(--t2);
	}
	.inl {
		display: grid;
		gap: 8px;
		padding: 4px 8px 10px;
	}
	.fl {
		display: grid;
		gap: 4px;
		font-size: 12px;
		color: var(--t2);
	}
	input {
		font: inherit;
		font-size: 13px;
		padding: 6px 8px;
		border-radius: 6px;
		border: 1px solid var(--border);
		background: var(--bg);
		color: var(--text);
		min-width: 0;
	}
	.bad {
		margin: 0;
		color: var(--red);
		font-size: 12px;
	}
	.found {
		margin: 0;
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: 13px;
	}
	.found span {
		color: var(--t3);
		font-size: 12px;
	}
	fieldset {
		border: 0;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 6px;
	}
	legend {
		font-size: 12px;
		color: var(--t2);
		padding: 0;
		margin-bottom: 6px;
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.chip {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		font: inherit;
		font-size: 12px;
		padding: 3px 9px;
		border-radius: 999px;
		border: 1px solid var(--border);
		background: var(--surface-2);
		color: var(--t2);
		cursor: pointer;
	}
	.chip.on {
		border-color: var(--accent);
		background: var(--accent-soft);
		color: var(--text);
	}
	.acts {
		display: flex;
		justify-content: flex-end;
		gap: 6px;
	}
	.btn {
		font: inherit;
		font-size: 12px;
		font-weight: 500;
		padding: 4px 10px;
		border-radius: 6px;
		border: 1px solid var(--border);
		background: var(--surface-2);
		color: var(--text);
		cursor: pointer;
	}
	.btn.primary {
		background: var(--accent-strong);
		border-color: var(--accent-strong);
		color: #fff;
	}
	.btn[disabled] {
		opacity: 0.45;
		cursor: default;
	}
	.main {
		display: grid;
		gap: 10px;
	}
	.ph {
		height: 44px;
		border-radius: 8px;
		background: var(--surface);
		border: 1px solid var(--border-soft);
	}
	.ph.tall {
		height: 112px;
	}
	.scrim {
		position: absolute;
		inset: 0;
		background: rgba(0, 0, 0, 0.55);
		display: grid;
		place-items: center;
		padding: 16px;
	}
	.dlg {
		width: min(420px, 100%);
		display: grid;
		gap: 10px;
		padding: 18px;
		border-radius: 12px;
		border: 1px solid var(--border);
		background: var(--surface-2);
	}
	.dlg h2 {
		margin: 0;
		font-size: 16px;
	}
	@media (max-width: 760px) {
		.frame {
			padding: 12px;
		}
		.cols {
			grid-template-columns: minmax(0, 1fr);
		}
		.tools {
			opacity: 1;
		}
	}
</style>
