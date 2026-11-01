<script lang="ts">
	import { st } from './_state.svelte';

	let { advanced = true }: { advanced?: boolean } = $props();

	const EVENTS = [
		{ k: 'claim', label: 'Drop claimed', sub: 'On any account.', tone: 'ok' },
		{ k: 'auth', label: 'Account needs login', sub: 'When a session expires.', tone: 'warn' },
		{ k: 'errors', label: 'Errors', sub: 'When mining stops on an error.', tone: 'bad' },
		{ k: 'daily', label: 'Daily summary', sub: 'Minutes and claims, at midnight.', tone: 'info' },
		{ k: 'codes', label: 'Reward codes', sub: 'New codes, and a reminder 2 days before they expire.', tone: 'violet' },
		{ k: 'progress', label: 'Drop progress', sub: 'Start, halfway and finish.', tone: 'info' },
		{ k: 'canary', label: 'Canary failures', sub: 'When watch time stops crediting.', tone: 'bad' }
	] as const;
</script>

<div class="panel">
	{#each EVENTS as e (e.k)}
		<div class="row" id="set-ev-{e.k}">
			<span class="dot {e.tone}" aria-hidden="true"></span>
			<div class="lbl">
				<b>{e.label}{#if e.k === 'codes'}<span class="chip hue plain">New</span>{/if}</b>
				<small>{e.sub}</small>
			</div>
			<button
				type="button"
				class="sw"
				role="switch"
				aria-checked={st.events[e.k]}
				aria-label={e.label}
				onclick={() => ((st.events[e.k] = !st.events[e.k]), st.flash())}
			></button>
		</div>
	{/each}
</div>

{#if advanced}
	<details class="adv">
		<summary>Advanced</summary>
		<div class="panel">
			<div class="card-body" id="set-avatar">
				<div class="inline">
					<div class="field grow">
						<label for="sx-avatar">Notification avatar URL</label>
						<input
							id="sx-avatar"
							class="inp"
							class:dirty={st.barMode && 'avatar' in st.dirty}
							placeholder="https://"
							value={st.avatar}
							oninput={(e) => st.set('avatar', e.currentTarget.value)}
						/>
					</div>
					{#if !st.barMode}<button type="button" class="btn" onclick={() => st.flash()}>Save</button>{/if}
				</div>
				<p class="help">Discord sender image (https). Empty: default.</p>
			</div>
			<div class="card-body" id="set-mention">
				<div class="inline">
					<div class="field grow">
						<label for="sx-mention">Mention on errors: Discord user ID</label>
						<input
							id="sx-mention"
							class="inp"
							class:dirty={st.barMode && 'mentionDiscord' in st.dirty}
							placeholder="Empty: no mention"
							value={st.mentionDiscord}
							oninput={(e) => st.set('mentionDiscord', e.currentTarget.value)}
						/>
					</div>
					{#if !st.barMode}<button type="button" class="btn" onclick={() => st.flash()}>Save</button>{/if}
				</div>
				<p class="help">Pinged only on auth problems and failures.</p>
			</div>
		</div>
	</details>
{/if}

<style>
	.dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		flex: none;
		background: var(--t3);
	}
	.dot.ok {
		background: var(--green);
	}
	.dot.warn {
		background: var(--amber);
	}
	.dot.bad {
		background: var(--red);
	}
	.dot.info {
		background: var(--blue);
	}
	.dot.violet {
		background: var(--accent);
	}
	.grow {
		flex: 1;
	}
	@media (max-width: 720px) {
		.row {
			flex-wrap: nowrap !important;
		}
	}
</style>
