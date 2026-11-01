<script lang="ts">
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import { LOGIN_LABEL } from './_data';
	import type { PanelState } from './_state.svelte';
	import Shell from './_Shell.svelte';
	import Login from './_Login.svelte';
	import Mining from './_Mining.svelte';
	import Points from './_Points.svelte';
	import Danger from './_Danger.svelte';
	import Saved from './_Saved.svelte';

	/** C: the row expands in place; the row is the header. */
	let { st }: { st: PanelState } = $props();

	function focusSelect(node: HTMLInputElement): void {
		node.focus();
		node.select();
	}
</script>

<Shell {st} inline>
	{#snippet below(a)}
		<div class="exp">
			<div class="strip">
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
					<span class="tag"
						><PlatformMark platform={a.platform} size="sm" />{a.platform === 'twitch' ? 'Twitch' : 'Kick'} · @{a.platform_name}</span
					>
					<span class="tag">{LOGIN_LABEL[a.login]}</span>
					<button type="button" class="lnk" onclick={() => st.startRename(a)}>Rename</button>
					<Saved {st} k="{a.id}:name" />
				{/if}
			</div>
			<div class="grid">
				<div class="cell"><Mining {st} {a} /></div>
				<div class="cell"><Login {st} {a} /></div>
				<div class="cell"><Points {a} /></div>
				<div class="cell"><Danger {st} {a} /></div>
			</div>
		</div>
	{/snippet}
</Shell>

<style>
	.exp {
		padding: 4px 16px 18px 62px;
		background: var(--surface-2);
		box-shadow: inset 2px 0 0 var(--accent);
		border-top: 0 !important;
	}
	.strip {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 8px;
		padding-bottom: 14px;
		font-size: 12.5px;
	}
	.rn {
		display: flex;
		gap: 6px;
		flex-wrap: wrap;
		flex: 1;
		max-width: 420px;
	}
	.rn .inp {
		flex: 1 1 160px;
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 12px;
	}
	.cell {
		padding: 14px 16px;
		border: 1px solid var(--border-soft);
		border-radius: 10px;
		background: var(--bg);
		min-width: 0;
	}
	@media (max-width: 720px) {
		.exp {
			padding: 2px 12px 14px;
		}
		.grid {
			grid-template-columns: minmax(0, 1fr);
		}
		.cell {
			padding: 12px;
		}
	}
</style>
