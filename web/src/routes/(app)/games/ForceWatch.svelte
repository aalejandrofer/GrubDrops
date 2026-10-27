<script lang="ts">
	import { tick } from 'svelte';
	import { api } from '$lib/app';
	import type { ForceChannel } from '$lib/api/types';
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { knownIds, moveChannel, parseChannel, toPut, togglePick, watchingIds } from '$lib/force-watch';
	import { t } from '$lib/i18n/index.svelte';
	import { getLive } from '$lib/live-context';

	/**
	 * Rail panel under the lists: channels idle accounts watch when no drop is
	 * up. Every change saves the whole ordered list.
	 */
	const live = getLive();
	const list = $derived(live.forceWatch ?? []);

	let panel = $state<HTMLElement | null>(null);
	let dlg = $state<HTMLDialogElement | null>(null);
	/** null = closed; 'new' = adding; else the key of the channel being edited. */
	let form = $state<string | null>(null);
	let url = $state('');
	let picked = $state<string[]>([]);
	let confirming = $state<string | null>(null);
	let saving = $state(false);
	let failed = $state(false);

	const key = (c: Pick<ForceChannel, 'platform' | 'channel'>) => `${c.platform}/${c.channel}`;
	const parsed = $derived(form === 'new' ? parseChannel(url) : null);
	const editing = $derived(form && form !== 'new' ? list.find((c) => key(c) === form) : undefined);
	const plat = $derived(editing?.platform ?? parsed?.platform);
	const choices = $derived(plat ? live.accounts.filter((a) => a.platform === plat) : []);
	const dupe = $derived(!!parsed && list.some((c) => c.platform === parsed.platform && c.channel === parsed.name));
	const urlErr = $derived(
		form === 'new' && url.trim() && !parsed ? t('force.url_bad') : dupe ? t('force.dupe') : ''
	);
	const canSave = $derived(picked.length > 0 && (!!editing || (!!parsed && !dupe)));

	const names = $derived(new Map(live.accounts.map((a) => [a.id, a.name])));
	const known = $derived(new Set(live.accounts.map((a) => a.id)));

	// The list shown with the save error. Any other list (an event reload or
	// the next save) clears the error.
	let failedList: ForceChannel[] | null = null;
	$effect(() => {
		if (failed && live.forceWatch !== failedList) failed = false;
	});

	/** CSS selector for a row's tool button. Channel keys hold only [a-z0-9_/-]. */
	const tool = (k: string, name: string) => `li[data-k="${k}"] [data-t="${name}"]`;
	const PLUS = '[data-t="plus"]';

	/**
	 * Saving disables the buttons and drops focus. Put it on the first enabled
	 * target, unless the user has moved focus elsewhere meanwhile.
	 */
	async function refocus(targets: string[]): Promise<void> {
		await tick();
		const at = document.activeElement;
		if (at && at !== document.body && !panel?.contains(at)) return;
		for (const q of targets) {
			const el = panel?.querySelector<HTMLButtonElement>(q);
			if (el && !el.disabled) {
				el.focus();
				return;
			}
		}
	}

	async function commit(next: ForceChannel[], focus: string[] = [PLUS]): Promise<void> {
		if (saving) return;
		saving = true;
		failed = false;
		confirming = null;
		const prev = live.forceWatch;
		live.forceWatch = next;
		try {
			live.forceWatch = (await api.putForceWatch(toPut(next, known))).channels;
		} catch {
			live.forceWatch = prev;
			try {
				live.forceWatch = (await api.forceWatch()).channels;
			} catch {
				/* the force_watch event refetches */
			}
			failedList = live.forceWatch;
			failed = true;
		} finally {
			saving = false;
		}
		await refocus(focus);
	}

	async function open(c: ForceChannel | null): Promise<void> {
		form = c ? key(c) : 'new';
		url = '';
		picked = c ? knownIds(c.accounts, known) : [];
		confirming = null;
		await tick();
		dlg?.showModal();
	}

	function save(): void {
		if (!canSave) return;
		const accounts = [...picked];
		const target = editing;
		const add = parsed;
		dlg?.close();
		if (target)
			void commit(
				list.map((c) => (key(c) === key(target) ? { ...c, accounts } : c)),
				[tool(key(target), 'edit'), PLUS]
			);
		else if (add)
			void commit([...list, { platform: add.platform, channel: add.name, live: null, accounts, watching: [] }]);
	}

	function move(i: number, d: number): void {
		const k = key(list[i]);
		const same = d < 0 ? 'up' : 'down';
		const other = d < 0 ? 'down' : 'up';
		void commit(moveChannel(list, i, d), [tool(k, same), tool(k, other), PLUS]);
	}

	async function askRemove(c: ForceChannel): Promise<void> {
		confirming = key(c);
		await tick();
		panel?.querySelector<HTMLButtonElement>(tool(key(c), 'confirm'))?.focus();
	}

	async function cancelRemove(c: ForceChannel): Promise<void> {
		confirming = null;
		await tick();
		panel?.querySelector<HTMLButtonElement>(tool(key(c), 'remove'))?.focus();
	}
</script>

<aside class="rail" aria-labelledby="fw-h" bind:this={panel}>
	<div class="head">
		<h2 class="rh" id="fw-h">{t('force.title')}</h2>
		<button type="button" class="plus" data-t="plus" aria-label={t('force.add')} title={t('force.add')} disabled={saving} onclick={() => open(null)}
			>+</button
		>
	</div>
	<p class="hint">{t('force.hint')}</p>
	{#if failed}<p class="bad" role="alert">{t('force.save_failed')}</p>{/if}
	{#if list.length === 0}
		<p class="empty">{t('force.empty')}</p>
	{:else}
		<ol>
			{#each list as c, i (key(c))}
				{@const now = watchingIds(c, live.runners)}
				<li data-k={key(c)}>
					<div class="row">
						<span class="rk">{i + 1}</span>
						<PlatformMark platform={c.platform} />
						<span class="who">
							<b
								><span class="nm">{c.channel}</span>{#if c.live !== null}<span
										class="dot"
										class:live={c.live}
										role="img"
										aria-label={c.live ? t('force.live') : t('force.offline')}
										title={c.live ? t('force.live') : t('force.offline')}
									></span>{/if}</b
							>
							<small>
								{#each c.accounts.filter((id) => names.has(id)) as id, k (id)}{k ? ' · ' : ''}<span
										class:now={now.includes(id)}>{names.get(id)}</span
									>{/each}
							</small>
						</span>
						<span class="tools" role="group" aria-label={c.channel}>
							<button
								type="button"
								class="ico"
								data-t="up"
								aria-label={t('force.move_up')}
								title={t('force.move_up')}
								disabled={i === 0 || saving}
								onclick={() => move(i, -1)}>↑</button
							>
							<button
								type="button"
								class="ico"
								data-t="down"
								aria-label={t('force.move_down')}
								title={t('force.move_down')}
								disabled={i === list.length - 1 || saving}
								onclick={() => move(i, 1)}>↓</button
							>
							<button
								type="button"
								class="ico"
								data-t="edit"
								aria-label={t('force.edit')}
								title={t('force.edit')}
								disabled={saving}
								onclick={() => open(c)}>✎</button
							>
							<button
								type="button"
								class="ico"
								data-t="remove"
								aria-label={t('force.remove')}
								title={t('force.remove')}
								disabled={saving}
								onclick={() => askRemove(c)}>×</button
							>
						</span>
					</div>
					{#if confirming === key(c)}
						<div class="conf">
							<span>{t('force.remove_confirm', { name: c.channel })}</span>
							<button
								type="button"
								class="danger"
								data-t="confirm"
								disabled={saving}
								onclick={() => commit(list.filter((x) => key(x) !== key(c)))}>{t('force.remove')}</button
							>
							<button type="button" class="plain" onclick={() => cancelRemove(c)}>{t('force.cancel')}</button>
						</div>
					{/if}
				</li>
			{/each}
		</ol>
	{/if}
</aside>

<dialog
	class="dlg"
	bind:this={dlg}
	aria-labelledby="fw-dlg-h"
	onclose={() => (form = null)}
	onclick={(e) => e.target === dlg && dlg?.close()}
>
	{#if form}
		<div class="body">
			<h2 id="fw-dlg-h">{editing ? t('force.edit_title', { name: editing.channel }) : t('force.add')}</h2>
			{#if form === 'new'}
				<label class="fl">
					<span>{t('force.url_label')}</span>
					<!-- svelte-ignore a11y_autofocus -->
					<input type="url" placeholder="https://www.twitch.tv/keshaeuw" bind:value={url} autofocus />
				</label>
				{#if urlErr}<p class="bad">{urlErr}</p>{/if}
				{#if parsed && !dupe}
					<p class="found">
						<PlatformMark platform={parsed.platform} /><b>{parsed.name}</b><span
							>{parsed.platform === 'twitch' ? t('force.twitch_channel') : t('force.kick_channel')}</span
						>
					</p>
				{/if}
			{/if}
			{#if plat}
				<fieldset>
					<legend>{t('force.accounts_label')}</legend>
					{#if choices.length}
						<div class="chips">
							{#each choices as a (a.id)}
								<button
									type="button"
									class="chip"
									class:on={picked.includes(a.id)}
									aria-pressed={picked.includes(a.id)}
									onclick={() => (picked = togglePick(picked, a.id))}
									><PlatformMark platform={a.platform} size="sm" />{a.name}</button
								>
							{/each}
						</div>
					{:else}
						<p class="bad">{plat === 'twitch' ? t('force.no_twitch') : t('force.no_kick')}</p>
					{/if}
				</fieldset>
			{/if}
			<div class="acts">
				<button type="button" class="btn" onclick={() => dlg?.close()}>{t('force.cancel')}</button>
				<button type="button" class="btn primary" disabled={!canSave || saving} onclick={save}
					>{editing ? t('force.save') : t('force.add_btn')}</button
				>
			</div>
		</div>
	{/if}
</dialog>

<style>
	.rail {
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--surface);
		padding: 8px;
	}
	.head {
		display: flex;
		align-items: center;
		justify-content: space-between;
	}
	.rh {
		margin: 8px 8px 4px;
		font-size: 11px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--t3);
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
	.plus:hover:not([disabled]) {
		background: var(--accent-soft);
	}
	.plus[disabled] {
		opacity: 0.45;
		cursor: default;
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
	li:hover {
		background: var(--surface-2);
	}
	.row {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 8px;
	}
	.rk {
		width: 12px;
		font-family: var(--mono);
		font-size: 11px;
		color: var(--dim);
		flex: none;
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
		min-width: 0;
	}
	.nm {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.who small {
		color: var(--t3);
		font-size: 12px;
	}
	.dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--surface-3);
		flex: none;
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
		flex: none;
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
	.plus:focus-visible,
	.ico:focus-visible,
	.chip:focus-visible,
	.btn:focus-visible,
	.conf button:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
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
	.conf button[disabled] {
		opacity: 0.45;
		cursor: default;
	}
	.conf .plain {
		border: 1px solid var(--border);
		background: none;
		color: var(--t2);
	}
	.bad {
		margin: 0 8px 6px;
		color: var(--red);
		font-size: 12px;
	}
	.dlg {
		width: min(420px, calc(100vw - 32px));
		max-height: calc(100dvh - 32px);
		padding: 18px;
		border: 1px solid var(--border);
		border-radius: 12px;
		background: var(--surface-2);
		color: var(--text);
	}
	.dlg::backdrop {
		background: rgb(0 0 0 / 0.55);
	}
	.body {
		display: grid;
		gap: 10px;
	}
	.body h2 {
		margin: 0;
		font-size: 16px;
	}
	.body .bad {
		margin: 0;
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
	input:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 0;
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
	@media (max-width: 760px) {
		.tools {
			opacity: 1;
		}
	}
</style>
