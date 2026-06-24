<script lang="ts">
  import { onMount } from 'svelte';
  import { apiFetch, apiSend } from '../lib/api';
  import type { SettingsView } from '../lib/types';

  let v = $state<SettingsView | null>(null);
  let webhook = $state('');
  let avatar = $state('');
  let notifyClaim = $state(false);
  let notifyProgress = $state(false);
  let notifyAuth = $state(false);
  let notifyError = $state(false);
  let notifyCanary = $state(false);
  let step = $state(25);
  let busy = $state(false);
  let testResult = $state('');

  async function load() {
    v = await apiFetch<SettingsView>('/api/settings');
    webhook = v.GlobalDiscordWebhook;
    avatar = v.NotifyAvatarURL;
    notifyClaim = v.NotifyClaim;
    notifyProgress = v.NotifyProgress;
    notifyAuth = v.NotifyAuth;
    notifyError = v.NotifyError;
    notifyCanary = v.NotifyCanary;
    step = v.ProgressNotifyStep;
  }
  onMount(load);

  async function save() {
    busy = true;
    try {
      await apiSend('/api/settings/notifications', 'POST', {
        discord_webhook: webhook,
        notify_avatar_url: avatar,
        notify_claim: notifyClaim,
        notify_progress: notifyProgress,
        notify_auth: notifyAuth,
        notify_error: notifyError,
        notify_canary: notifyCanary,
        progress_notify_step: step,
      });
      await load();
    } finally { busy = false; }
  }

  async function sendTest() {
    testResult = 'Sending…';
    const res = await apiSend<{ ok: boolean; message?: string }>('/api/settings/notify-test', 'POST');
    testResult = res?.ok ? 'Test sent!' : (res?.message ?? 'Failed');
    setTimeout(() => { testResult = ''; }, 4000);
  }
</script>

{#if v}
  <form class="settings-form" onsubmit={(e) => { e.preventDefault(); save(); }}>
    <label>Discord webhook URL<input type="url" bind:value={webhook} placeholder="https://discord.com/api/webhooks/…" /></label>
    <label>Notification avatar URL<input type="url" bind:value={avatar} placeholder="https://…/avatar.png" /></label>
    <label class="checkbox"><input type="checkbox" bind:checked={notifyClaim} /> Notify on drop claim</label>
    <label class="checkbox"><input type="checkbox" bind:checked={notifyProgress} /> Notify on progress milestones</label>
    <label>Progress notify step (%)<input type="number" bind:value={step} min="1" max="100" /></label>
    <label class="checkbox"><input type="checkbox" bind:checked={notifyAuth} /> Notify on auth events</label>
    <label class="checkbox"><input type="checkbox" bind:checked={notifyError} /> Notify on errors</label>
    <label class="checkbox"><input type="checkbox" bind:checked={notifyCanary} /> Notify on canary checks</label>
    <div class="form-actions">
      <button type="submit" disabled={busy}>Save</button>
      <button type="button" onclick={sendTest}>Send test</button>
      {#if testResult}<span class="note">{testResult}</span>{/if}
    </div>
  </form>
{:else}
  <p class="loading">Loading…</p>
{/if}
