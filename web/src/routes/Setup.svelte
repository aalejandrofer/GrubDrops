<script lang="ts">
  import { onMount } from 'svelte';
  import { apiFetch, apiSend, ApiError } from '../lib/api';
  import { navigate } from '../lib/router.svelte';
  import type { AuthInfo } from '../lib/types';

  let password = $state('');
  let confirm = $state('');
  let busy = $state(false);
  let error = $state<string | null>(null);

  onMount(async () => {
    try {
      const info = await apiFetch<AuthInfo>('/api/auth/info');
      if (info.admin_exists) {
        navigate('/login');
      }
    } catch { /* leave on page */ }
  });

  async function submit() {
    error = null;
    if (password.length < 8) {
      error = 'Password must be at least 8 characters';
      return;
    }
    if (password !== confirm) {
      error = 'Passwords do not match';
      return;
    }
    busy = true;
    try {
      await apiSend('/api/setup', 'POST', { password, confirm });
      window.location.assign('/');
    } catch (e) {
      error = e instanceof ApiError ? e.message : 'Setup failed';
      busy = false;
    }
  }
</script>

<div class="auth-wrap">
  <div class="auth-card auth-centered auth-bare">
    <img class="auth-logo" src="/static/img/icon-512.png" alt="GrubDrops" width="96" height="96" />
    <h1 class="auth-name">GRUB<span class="dot"></span>DROPS</h1>
    <p class="auth-sub">Create admin password</p>
    {#if error}<div class="err">{error}</div>{/if}
    <form class="auth-secondary" onsubmit={(e) => { e.preventDefault(); submit(); }}>
      <div class="login-row">
        <input type="password" bind:value={password} placeholder="Password" required autofocus />
      </div>
      <div class="login-row">
        <input type="password" bind:value={confirm} placeholder="Confirm password" required />
        <button class="btn-arrow" type="submit" disabled={busy} aria-label="Set password">→</button>
      </div>
    </form>
  </div>
</div>
