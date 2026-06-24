<script lang="ts">
  import { onMount } from 'svelte';
  import { apiFetch, apiSend, ApiError } from '../lib/api';

  let password = $state('');
  let busy = $state(false);
  let error = $state<string | null>(null);
  let oidc = $state<{ enabled: boolean; provider: string }>({ enabled: false, provider: '' });

  onMount(async () => {
    try {
      const info = await apiFetch<{ oidc_enabled: boolean; oidc_provider: string }>('/api/auth/info');
      oidc = { enabled: info.oidc_enabled, provider: info.oidc_provider };
    } catch { /* leave defaults */ }
  });

  async function submit() {
    busy = true; error = null;
    try {
      await apiSend('/api/login', 'POST', { password });
      window.location.assign('/');
    } catch (e) {
      error = e instanceof ApiError ? e.message : 'Login failed';
      busy = false;
    }
  }
</script>

<div class="auth-wrap">
  <div class="auth-card auth-centered auth-bare">
    <img class="auth-logo" src="/static/img/icon-512.png" alt="GrubDrops" width="96" height="96" />
    <h1 class="auth-name">GRUB<span class="dot"></span>DROPS</h1>
    {#if error}<div class="err">{error}</div>{/if}
    {#if oidc.enabled}
      <a class="btn-sso" href="/auth/oidc/login">{oidc.provider || 'Sign in with SSO'}</a>
      <div class="sso-divider"><span>or</span></div>
    {/if}
    <form class="auth-secondary" onsubmit={(e) => { e.preventDefault(); submit(); }}>
      <div class="login-row">
        <input type="password" bind:value={password} placeholder="Password" required autofocus />
        <button class="btn-arrow" type="submit" disabled={busy} aria-label="Log in">→</button>
      </div>
    </form>
  </div>
</div>
