<script lang="ts">
	import Ico from './_Ico.svelte';
	import { st } from './_state.svelte';
	import OidcGuide from './_OidcGuide.svelte';

	let { part = 'all' }: { part?: 'all' | 'password' | 'backup' } = $props();
	let guide = $state(false);
</script>

{#if part !== 'backup'}
<div class="panel">
	<div class="row" id="set-sso">
		<div class="lbl">
			<b
				>Single sign-on {#if st.sso}<span class="chip ok">authentik</span>{:else}<span class="chip off">Not set up</span>{/if}</b
			>
			<small>{st.sso ? 'auth.example.com · 2 emails allowed · Set in env' : 'Sign in with authentik, Google or any OIDC provider.'}</small>
		</div>
		{#if !st.sso}<button type="button" class="btn" onclick={() => (guide = true)}>Set up</button>{/if}
		<!-- Mock only: flips the SSO state to show both versions. -->
		<button type="button" class="btn ghost" onclick={() => ((st.sso = !st.sso), st.sso || (st.pwLogin = true))}
			>Mock: {st.sso ? 'unset' : 'set'}</button
		>
	</div>
	<div class="row" id="set-pw-login">
		<div class="lbl">
			<b>Password sign-in</b>
			<small
				>{!st.sso
					? 'Optional. Turn off once single sign-on is set up.'
					: st.pwLogin
						? 'Optional. Turn off to sign in with authentik only.'
						: 'authentik only. If it is down, re-enable from the host.'}</small
			>
		</div>
		{#if st.sso}
			<button
				type="button"
				class="sw"
				role="switch"
				aria-checked={st.pwLogin}
				aria-label="Password sign-in"
				onclick={() => ((st.pwLogin = !st.pwLogin), st.flash())}
			></button>
		{:else}
			<span class="chip off">OIDC not enabled</span>
		{/if}
	</div>
	<div class="card-body pwform" id="set-pw-change">
		<div class="field">
			<label for="sx-pw0">Current password</label>
			<input id="sx-pw0" class="inp" type="password" autocomplete="off" />
		</div>
		<div class="two">
			<div class="field">
				<label for="sx-pw1">New password</label>
				<input id="sx-pw1" class="inp" type="password" autocomplete="off" />
			</div>
			<div class="field">
				<label for="sx-pw2">Confirm new password</label>
				<input id="sx-pw2" class="inp" type="password" autocomplete="off" />
			</div>
		</div>
		<div><button type="button" class="btn">Change password</button></div>
	</div>
</div>
<OidcGuide bind:open={guide} />
{/if}

{#if part !== 'password'}
	<div class="sub-h"><span class="tile sm"><Ico name="download" size={13} /></span>Backup</div>
	<div class="panel">
		<div class="row" id="set-snapshot">
			<div class="lbl">
				<b>Database snapshot</b>
				<small>Sessions stay encrypted with your master key. Keep it private.</small>
			</div>
			<button type="button" class="btn"><Ico name="download" size={14} /> Download</button>
		</div>
	</div>
{/if}

<style>
	/* Password fields stay a readable width on wide screens. */
	.pwform {
		grid-template-columns: minmax(0, 560px);
	}
</style>
