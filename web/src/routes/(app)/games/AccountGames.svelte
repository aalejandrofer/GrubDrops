<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { api } from '$lib/app';
	import type { Account, AccountGame, SearchHit } from '$lib/api/types';
	import { addGame, sameList, startList } from '$lib/account-games';
	import { matchParts, suggestions } from '$lib/games';
	import { t } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';

	/** Editor for one account's own game list, shown in a dialog. Saving turns the list on. */
	let { account, onsaved, oncancel }: { account: Account; onsaved: () => void; oncancel: () => void } = $props();

	const live = getLive();
	let loaded = $state(false);
	let loadFailed = $state(false);
	let list = $state<AccountGame[]>([]);
	// What the server holds, for the unsaved check.
	let savedOwn = false;
	let saved: AccountGame[] = [];
	let saving = $state(false);
	let saveFailed = $state(false);
	let q = $state('');
	let hits = $state<SearchHit[]>([]);
	let seq = 0;
	let timer: ReturnType<typeof setTimeout> | undefined;
	onDestroy(() => clearTimeout(timer));

	// Turning the list on is a change even when the copied list is untouched.
	const dirty = $derived(loaded && (!savedOwn || !sameList(list, saved)));
	const sugg = $derived(suggestions(hits, q));
	const listed = (name: string, id = '') =>
		list.some((g) => (id !== '' && g.id === id) || g.name.trim().toLowerCase() === name.trim().toLowerCase());

	onMount(async () => {
		try {
			const [mine, global] = await Promise.all([
				api.accountGames(account.id),
				live.games ? Promise.resolve(live.games) : api.games()
			]);
			savedOwn = mine.own;
			saved = mine.games;
			list = startList(mine, global.games);
			loaded = true;
		} catch {
			loadFailed = true;
		}
	});

	function onInput(): void {
		clearTimeout(timer);
		const text = q.trim();
		const mine = ++seq;
		if (!text) {
			hits = [];
			return;
		}
		timer = setTimeout(async () => {
			try {
				const r = await api.searchGames(text);
				if (mine === seq) hits = r;
			} catch {
				if (mine === seq) hits = [];
			}
		}, 200);
	}

	function add(g: AccountGame): void {
		list = addGame(list, g);
		q = '';
		hits = [];
		seq++;
	}

	function move(i: number, dir: -1 | 1): void {
		const j = i + dir;
		if (j < 0 || j >= list.length) return;
		const next = [...list];
		[next[i], next[j]] = [next[j], next[i]];
		list = next;
	}

	async function save(): Promise<void> {
		saving = true;
		saveFailed = false;
		try {
			await api.setAccountGames(account.id, true, list);
			onsaved();
		} catch {
			saveFailed = true;
		} finally {
			saving = false;
		}
	}
</script>

{#if loadFailed}
	<p class="bad" role="alert">{t('accounts.games_load_failed')}</p>
{:else if loaded}
	<p class="sub">{t('accounts.games_own_sub')}</p>
	{#if list.length === 0}
		<p class="empty">{t('accounts.games_empty')}</p>
	{:else}
		<ol class="gl">
			{#each list as g, i (g.id || `n:${g.name}`)}
				<li>
					<span class="n">{i + 1}</span>
					<span class="nm">{g.name}</span>
					<button type="button" class="mini" aria-label={t('accounts.games_up')} title={t('accounts.games_up')} disabled={i === 0} onclick={() => move(i, -1)}>
						<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m6 15 6-6 6 6" /></svg>
					</button>
					<button
						type="button"
						class="mini"
						aria-label={t('accounts.games_down')}
						title={t('accounts.games_down')}
						disabled={i === list.length - 1}
						onclick={() => move(i, 1)}
					>
						<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg>
					</button>
					<button
						type="button"
						class="mini"
						aria-label={t('accounts.games_remove', { game: g.name })}
						title={t('accounts.games_remove', { game: g.name })}
						onclick={() => (list = list.filter((_, j) => j !== i))}
					>
						<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12" /></svg>
					</button>
				</li>
			{/each}
		</ol>
	{/if}
	<input
		class="inp search"
		type="search"
		bind:value={q}
		oninput={onInput}
		onkeydown={(e) => {
			if (e.key === 'Enter' && q.trim()) {
				e.preventDefault();
				add({ id: '', name: q });
			}
		}}
		placeholder={t('games.add_name_ph')}
		aria-label={t('games.add_label')}
		autocomplete="off"
	/>
	{#if sugg.length > 0}
		<ul class="sugg">
			{#each sugg as s (s.kind === 'hit' ? `h:${s.hit.id}` : 'add')}
				<li>
					{#if s.kind === 'hit'}
						{@const m = matchParts(s.hit.name, q)}
						{@const has = listed(s.hit.name, s.hit.id)}
						<span class="nm">{m.pre}{#if m.hit}<mark>{m.hit}</mark>{/if}{m.post}</span>
						<button type="button" class="btn" disabled={has} onclick={() => add({ id: s.hit.id, name: s.hit.name })}
							>{has ? t('games.added') : t('games.add')}</button
						>
					{:else}
						<span class="nm">{t('games.add_named', { name: s.name })}</span>
						<button type="button" class="btn" disabled={listed(s.name)} onclick={() => add({ id: '', name: s.name })}>{t('games.add')}</button>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}

	{#if saveFailed}<p class="bad" role="alert">{t('accounts.games_failed')}</p>{/if}
	<div class="foot">
		<button type="button" class="btn" onclick={oncancel}>{t('accounts.cancel')}</button>
		<button type="button" class="btn primary" disabled={!dirty || saving} onclick={save}>{t('accounts.save')}</button>
	</div>
{/if}

<style>
	.sub {
		margin: 0 0 14px;
		color: var(--t2);
		font-size: 13px;
	}
	.empty {
		margin: 14px 0 0;
		font-size: 13px;
		color: var(--t2);
	}
	.bad {
		margin: 12px 0 0;
		color: var(--red);
		font-size: 12.5px;
	}
	.gl,
	.sugg {
		list-style: none;
		margin: 14px 0 0;
		padding: 0;
		border: 1px solid var(--border-soft);
		border-radius: 8px;
		max-height: 260px;
		overflow: auto;
	}
	.gl li,
	.sugg li {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 6px 8px 6px 10px;
		font-size: 13px;
	}
	.gl li + li,
	.sugg li + li {
		border-top: 1px solid var(--border-soft);
	}
	.n {
		width: 18px;
		font: 12px var(--mono);
		color: var(--t3);
		text-align: right;
		flex: none;
	}
	.nm {
		flex: 1;
		min-width: 0;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.nm mark {
		background: none;
		color: var(--accent);
	}
	.mini {
		width: 24px;
		height: 24px;
		display: grid;
		place-items: center;
		padding: 0;
		border: 1px solid transparent;
		border-radius: 5px;
		background: none;
		color: var(--t2);
		cursor: pointer;
		flex: none;
	}
	.mini svg {
		width: 14px;
		height: 14px;
		fill: none;
		stroke: currentColor;
		stroke-width: 2;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
	.mini:hover:not(:disabled) {
		border-color: var(--border);
		color: var(--text);
		background: var(--surface-2);
	}
	.mini:disabled {
		opacity: 0.35;
		cursor: default;
	}
	.search {
		width: 100%;
		margin-top: 12px;
		box-sizing: border-box;
	}
	.inp {
		height: 34px;
		padding: 0 10px;
		border: 1px solid var(--border);
		border-radius: 7px;
		background: var(--surface);
		color: var(--text);
		font: 13px var(--font);
	}
	.inp:focus {
		outline: 0;
		border-color: var(--accent);
		box-shadow: 0 0 0 3px var(--accent-soft);
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
		white-space: nowrap;
		cursor: pointer;
	}
	.btn:hover:not(:disabled) {
		border-color: var(--t2);
	}
	.btn:disabled {
		opacity: 0.6;
		cursor: default;
	}
	.btn:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.btn.primary {
		background: var(--accent-strong);
		border-color: var(--accent-strong);
		color: #fff;
	}
	.btn.primary:hover:not(:disabled) {
		background: color-mix(in srgb, var(--accent-strong) 85%, #fff);
	}
	.foot {
		display: flex;
		justify-content: flex-end;
		gap: 8px;
		margin-top: 18px;
	}
</style>
