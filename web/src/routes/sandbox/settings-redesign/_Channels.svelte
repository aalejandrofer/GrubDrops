<script lang="ts">
	import Ico from './_Ico.svelte';
	import { st } from './_state.svelte';

	let tgOpen = $state(false);
</script>

<div class="panel">
	<div class="card-body" id="set-discord">
		<div class="title">
			<span class="brand discord"><Ico name="chat" size={14} /></span>
			<b>Discord</b>
			{#if st.discord}<span class="chip ok">Connected</span>{:else}<span class="chip off">Off</span>{/if}
		</div>
		<div class="inline">
			<div class="field grow">
				<label for="sx-hook">Webhook URL</label>
				<input id="sx-hook" class="inp" value="https://discord.com/api/webhooks/1301…/••••••••" disabled />
			</div>
			<button type="button" class="btn">Change</button>
		</div>
	</div>
	<div class="card-body" id="set-telegram">
		<div class="title">
			<span class="brand telegram"><Ico name="send" size={13} /></span>
			<b>Telegram</b>
			{#if st.telegram}<span class="chip ok">Connected</span>{:else}<span class="chip off">Not connected</span>{/if}
			{#if !tgOpen && !st.telegram}<button type="button" class="btn tint push" onclick={() => (tgOpen = true)}>Connect</button>{/if}
		</div>
		{#if tgOpen || st.telegram}
			<div class="two">
				<div class="field">
					<label for="sx-tok">Bot token</label>
					<input
						id="sx-tok"
						class="inp"
						class:dirty={st.barMode && 'tgToken' in st.dirty}
						placeholder="123456:ABC-DEF"
						value={st.tgToken}
						oninput={(e) => st.set('tgToken', e.currentTarget.value)}
					/>
				</div>
				<div class="field">
					<label for="sx-chat">Chat ID</label>
					<input
						id="sx-chat"
						class="inp"
						class:dirty={st.barMode && 'tgChat' in st.dirty}
						placeholder="-1001234567890"
						value={st.tgChat}
						oninput={(e) => st.set('tgChat', e.currentTarget.value)}
					/>
				</div>
			</div>
			<p class="help">Get a bot token from @BotFather. Message the bot, then read chat.id from getUpdates.</p>
			{#if !st.barMode}
				<div class="inline">
					<button type="button" class="btn" onclick={() => ((st.telegram = true), (tgOpen = false), st.flash())}>Save</button>
					<button type="button" class="btn ghost" onclick={() => (tgOpen = false)}>Cancel</button>
				</div>
			{/if}
		{/if}
	</div>
</div>

<div class="panel">
	<div class="row" id="set-test">
		<div class="lbl">
			<b>Test notification</b>
			<small>Sends a sample drop message.</small>
		</div>
		<div class="inline">
			<button type="button" class="btn" disabled={!st.discord}>Send to Discord</button>
			<button type="button" class="btn" disabled={!st.telegram}>Send to Telegram</button>
		</div>
	</div>
</div>

<style>
	.title {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
	}
	.title b {
		font-size: 13.5px;
		font-weight: 500;
	}
	.brand {
		width: 24px;
		height: 24px;
		border-radius: 7px;
		display: grid;
		place-items: center;
		color: #fff;
	}
	.discord {
		background: var(--discord);
	}
	.telegram {
		background: var(--telegram);
	}
	.push {
		margin-left: auto;
	}
	.grow {
		flex: 1;
	}
	.inp:disabled {
		color: var(--t2);
		background: var(--surface);
	}
	@media (max-width: 420px) {
		.inline {
			flex-wrap: wrap;
		}
	}
</style>
