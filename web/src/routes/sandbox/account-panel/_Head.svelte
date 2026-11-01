<script lang="ts">
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { initials, line, LOGIN_LABEL, status, STATUS_LABEL, type Acct } from './_data';
	import type { PanelState } from './_state.svelte';
	import Saved from './_Saved.svelte';

	/** Detail header: avatar, name (rename in place), platform, login type, status. */
	let { st, a }: { st: PanelState; a: Acct } = $props();
	const s = $derived(status(a));

	function focusSelect(node: HTMLInputElement): void {
		node.focus();
		node.select();
	}
</script>

<div class="hd">
	<span class="av big">
		{initials(a.name)}
		<span class="mk"><PlatformMark platform={a.platform} /></span>
	</span>
	<div class="who">
		{#if st.renaming === a.id}
			<div class="rn">
				<input
					class="inp"
					aria-label="Rename {a.name}"
					placeholder={a.platform_name}
					maxlength={40}
					bind:value={st.renameValue}
					use:focusSelect
					onkeydown={(e) => {
						if (e.key === 'Enter') st.commitRename(a);
						else if (e.key === 'Escape') {
							e.stopPropagation();
							st.renaming = null;
						}
					}}
				/>
				<button type="button" class="btn primary" onclick={() => st.commitRename(a)}>Save</button>
				<button type="button" class="btn" onclick={() => (st.renaming = null)}>Cancel</button>
			</div>
		{:else}
			<div class="nm">
				<h3>{a.name}</h3>
				<button type="button" class="ico sm" aria-label="Rename {a.name}" title="Rename" onclick={() => st.startRename(a)}>
					<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M4 20h4L19 9l-4-4L4 16v4zM14 6l4 4" /></svg>
				</button>
				<Saved {st} k="{a.id}:name" />
			</div>
		{/if}
		<div class="tags">
			<span class="tag"><PlatformMark platform={a.platform} size="sm" />{a.platform === 'twitch' ? 'Twitch' : 'Kick'} · @{a.platform_name}</span>
			<span class="tag">{LOGIN_LABEL[a.login]}</span>
			<span class="st {s}">{STATUS_LABEL[s]}</span>
		</div>
		<p class="ln" class:bad={s === 'signed_out'}>{line(a)}</p>
	</div>
</div>

<style>
	.hd {
		display: flex;
		gap: 14px;
		align-items: flex-start;
		min-width: 0;
	}
	.av.big {
		width: 44px;
		height: 44px;
		font-size: 15px;
	}
	.who {
		flex: 1;
		min-width: 0;
	}
	.nm {
		display: flex;
		align-items: center;
		gap: 4px;
		min-width: 0;
	}
	h3 {
		margin: 0;
		font-size: 17px;
		font-weight: 600;
		letter-spacing: -0.01em;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.ico.sm {
		width: 26px;
		height: 26px;
	}
	.rn {
		display: flex;
		gap: 6px;
		align-items: center;
		flex-wrap: wrap;
	}
	.rn .inp {
		flex: 1 1 160px;
	}
	.tags {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		margin-top: 6px;
	}
	.ln {
		margin: 6px 0 0;
		font-size: 12.5px;
		color: var(--t2);
	}
	.ln.bad {
		color: color-mix(in srgb, var(--red) 70%, var(--text));
	}
</style>
