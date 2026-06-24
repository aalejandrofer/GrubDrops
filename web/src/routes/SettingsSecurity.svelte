<script lang="ts">
  import { onMount } from 'svelte';
  import { apiFetch, apiSend, ApiError } from '../lib/api';
  import type { SettingsView } from '../lib/types';

  let v = $state<SettingsView | null>(null);
  let currentPassword = $state('');
  let newPassword = $state('');
  let confirmPassword = $state('');
  let busy = $state(false);
  let successMsg = $state('');
  let errorMsg = $state('');

  async function load() {
    v = await apiFetch<SettingsView>('/api/settings');
  }
  onMount(load);

  async function changePassword(e: Event) {
    e.preventDefault();
    busy = true;
    successMsg = '';
    errorMsg = '';
    try {
      await apiSend('/api/settings/password', 'POST', {
        current_password: currentPassword,
        new_password: newPassword,
        confirm_password: confirmPassword,
      });
      successMsg = 'Password changed';
      currentPassword = '';
      newPassword = '';
      confirmPassword = '';
    } catch (err) {
      errorMsg = err instanceof ApiError ? err.message : 'An unexpected error occurred';
    } finally {
      busy = false;
    }
  }
</script>

<div class="settings-security">
  <section class="settings-section">
    <h2>Change Password</h2>
    <form class="settings-form" onsubmit={changePassword}>
      <label for="current-password">Current Password</label>
      <input id="current-password" type="password" bind:value={currentPassword} autocomplete="current-password" />
      <label for="new-password">New Password</label>
      <input id="new-password" type="password" bind:value={newPassword} autocomplete="new-password" />
      <label for="confirm-password">Confirm New Password</label>
      <input id="confirm-password" type="password" bind:value={confirmPassword} autocomplete="new-password" />
      <div class="form-actions">
        <button type="submit" disabled={busy}>Change Password</button>
        {#if successMsg}<span class="note ok">{successMsg}</span>{/if}
        {#if errorMsg}<span class="note err">{errorMsg}</span>{/if}
      </div>
    </form>
  </section>

  {#if v}
    <section class="settings-section">
      <h2>Single Sign-On (OIDC)</h2>
      {#if v.OIDC?.Enabled}
        <dl class="info-list">
          <dt>Provider</dt><dd>{v.OIDC.ProviderName}</dd>
          <dt>Issuer</dt><dd>{v.OIDC.Issuer}</dd>
          <dt>Callback URL</dt><dd>{v.OIDC.CallbackURL}</dd>
          {#if v.OIDC.AllowedEmails && v.OIDC.AllowedEmails.length > 0}
            <dt>Allowed Emails</dt><dd>{v.OIDC.AllowedEmails.join(', ')}</dd>
          {/if}
          {#if v.OIDC.AllowedGroups && v.OIDC.AllowedGroups.length > 0}
            <dt>Allowed Groups</dt><dd>{v.OIDC.AllowedGroups.join(', ')}</dd>
          {/if}
        </dl>
      {:else}
        <p class="note">SSO is not enabled.</p>
      {/if}
    </section>
  {:else}
    <p class="loading">Loading…</p>
  {/if}
</div>
