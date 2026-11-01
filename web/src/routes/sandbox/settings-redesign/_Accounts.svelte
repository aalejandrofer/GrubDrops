<script lang="ts">
	import PlatformMark from '$lib/components/PlatformMark.svelte';
	import Ico from './_Ico.svelte';
	import { STATUS_LABEL, type Status } from './_data';
	import { st } from './_state.svelte';

	const TONE: Record<Status, string> = { mining: 'ok', points: 'violet', paused: 'off', signed_out: 'bad' };

	function login(id: string): void {
		const a = st.accounts.find((x) => x.id === id);
		if (!a) return;
		a.status = 'mining';
		a.line = 'Mining: Rust';
		st.flash('Signed in');
	}
</script>

<div class="panel" id="set-acc-list">
	<div class="row top">
		<div class="lbl">
			<b>
				<span class="count twitch"><PlatformMark platform="twitch" /> {st.counts.twitch} Twitch</span>
				<span class="count kick"><PlatformMark platform="kick" /> {st.counts.kick} Kick</span>
			</b>
		</div>
		<button type="button" class="btn tint" id="set-acc-add"><Ico name="plus" size={14} /> Add account</button>
	</div>
	{#each st.accounts as a (a.id)}
		<div class="row acct {a.platform}">
			<span class="av" aria-hidden="true">{a.name.slice(0, 1)}</span>
			<div class="lbl">
				<b><PlatformMark platform={a.platform} /> <span class="pf-name {a.platform}">{a.name}</span> <span class="via">{a.login}</span></b>
				<small class:bad={a.status === 'signed_out'}>{a.line}</small>
			</div>
			<span class="chip {TONE[a.status]}">{STATUS_LABEL[a.status]}</span>
			{#if a.status === 'signed_out'}
				<button type="button" class="btn primary" onclick={() => login(a.id)}>Log in again</button>
			{:else}
				<button type="button" class="btn ghost" aria-label="Open {a.name}"><Ico name="chevron-right" size={15} /></button>
			{/if}
		</div>
	{/each}
</div>

<style>
	.top {
		padding-top: 10px;
		padding-bottom: 10px;
	}
	.count {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font-size: 13px;
		color: var(--t2);
	}
	.count + .count {
		margin-left: 10px;
	}
	.av {
		width: 32px;
		height: 32px;
		border-radius: 50%;
		display: grid;
		place-items: center;
		font-weight: 600;
		font-size: 13px;
		flex: none;
	}
	.twitch .av {
		color: var(--twitch-av);
		background: color-mix(in srgb, var(--twitch) 18%, transparent);
	}
	.kick .av {
		color: var(--kick-fg);
		background: color-mix(in srgb, var(--kick) 14%, transparent);
	}
	.via {
		font-size: 11.5px;
		font-weight: 500;
		color: var(--t3);
		padding: 0 6px;
		border: 1px solid var(--border);
		border-radius: 5px;
	}
	small.bad {
		color: var(--red) !important;
	}
	@media (max-width: 720px) {
		.acct {
			flex-wrap: nowrap !important;
		}
		.acct .chip {
			display: none;
		}
		.via {
			display: none;
		}
	}
</style>
