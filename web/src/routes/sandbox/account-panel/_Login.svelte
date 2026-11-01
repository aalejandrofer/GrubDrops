<script lang="ts">
	import { LOGIN_LABEL, type Acct } from './_data';
	import type { PanelState } from './_state.svelte';

	/** Login: how the account signed in, session health, check and sign in again. */
	let { st, a, bare = false }: { st: PanelState; a: Acct; bare?: boolean } = $props();

	const flowing = $derived(st.loggingIn === a.id);
	const n = $derived(st.note[`${a.id}:login`]);
</script>

<section class="sec" aria-label="Login">
	{#if !bare}<h4 class="sh">Login</h4>{/if}
	<dl class="kv">
		<dt>Signed in with</dt>
		<dd>{LOGIN_LABEL[a.login]} · since {a.signedIn}</dd>
		<dt>Platform login</dt>
		<dd class="mono">@{a.platform_name}</dd>
		<dt>Session</dt>
		<dd>
			{#if a.needs_login}
				<span class="bad">Expired</span> · {a.check.detail} · checked {a.check.ago}
			{:else}
				<span class="good">Valid</span> · checked {a.check.ago}
			{/if}
		</dd>
	</dl>
	{#if a.needs_login && !flowing}
		<p class="warn">Mining stopped for this account. Sign in again to resume; its games and settings stay.</p>
	{/if}

	{#if flowing}
		<div class="flow">
			{#if a.login === 'tv'}
				<p>Open <span class="mono">twitch.tv/activate</span> and enter this code:</p>
				<p class="code mono">WXYZ-K7QP</p>
				<p class="wait">Waiting for Twitch…</p>
			{:else if a.login === 'cookies'}
				<p>Export cookies for kick.com in Netscape format and pick the file.</p>
				<button type="button" class="btn">Choose a file</button>
			{:else}
				<p>Sign in on the Twitch app page, then paste the redirect URL here.</p>
				<input class="inp mono" placeholder="https://…#access_token=…" aria-label="Redirect URL" />
			{/if}
			<div class="acts">
				<button type="button" class="btn primary" onclick={() => st.finishLogin(a)}>Simulate sign-in</button>
				<button type="button" class="btn" onclick={() => (st.loggingIn = null)}>Cancel</button>
			</div>
		</div>
	{:else}
		<div class="acts">
			<button type="button" class="btn" disabled={n?.tone === 'wait'} onclick={() => st.check(a)}>Check login now</button>
			<button type="button" class="btn" class:primary={a.needs_login} onclick={() => st.relogin(a)}>Log in again</button>
		</div>
	{/if}
	{#if n}<p class="note {n.tone}" role="status">{n.text}</p>{/if}
</section>

<style>
	.good {
		color: var(--green);
		font-weight: 500;
	}
	.bad {
		color: var(--red);
		font-weight: 500;
	}
	.warn {
		margin: 0;
		font-size: 12.5px;
		padding: 8px 10px;
		border-radius: 7px;
		background: var(--red-soft);
		color: color-mix(in srgb, var(--red) 70%, var(--text));
	}
	.flow {
		display: grid;
		gap: 8px;
		padding: 12px;
		border: 1px dashed var(--border);
		border-radius: 8px;
		background: var(--bg);
		font-size: 13px;
	}
	.flow p {
		margin: 0;
	}
	.code {
		font-size: 20px;
		font-weight: 600;
		letter-spacing: 0.08em;
		color: var(--accent);
	}
	.wait {
		color: var(--t3);
		font-size: 12.5px;
	}
</style>
