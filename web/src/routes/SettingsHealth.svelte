<script lang="ts">
  import { onMount } from 'svelte';
  import { apiFetch, apiSend } from '../lib/api';
  import type { SettingsView } from '../lib/types';

  let v = $state<SettingsView | null>(null);
  let twitchChannel = $state('');
  let kickChannel = $state('');
  let intervalSec = $state(21600);
  let busy = $state(false);
  let running = $state(false);

  async function load() {
    v = await apiFetch<SettingsView>('/api/settings');
    twitchChannel = v.CanaryTwitchChannel;
    kickChannel = v.CanaryKickChannel;
    intervalSec = v.CanaryIntervalSec;
  }
  onMount(load);

  async function save(e: Event) {
    e.preventDefault();
    busy = true;
    try {
      await apiSend('/api/settings/canary', 'POST', {
        canary_twitch_channel: twitchChannel,
        canary_kick_channel: kickChannel,
        canary_interval_sec: intervalSec,
      });
      await load();
    } finally { busy = false; }
  }

  async function runNow() {
    running = true;
    try {
      await apiSend('/api/settings/canary/run', 'POST');
      await load();
    } finally { running = false; }
  }
</script>

<div class="settings-health">
  {#if v}
    <section class="settings-section">
      <h2>Heartbeat Health Checker</h2>
      <div class="canary-results">
        <div class="canary-result">
          <strong>Twitch</strong>
          {#if v.CanaryTwitch?.Configured}
            <span class="canary-status {v.CanaryTwitch.OK ? 'ok' : 'fail'}">{v.CanaryTwitch.OK ? 'OK' : 'FAIL'}</span>
            <span class="canary-detail">{v.CanaryTwitch.Detail}</span>
            {#if v.CanaryTwitch.When}<span class="canary-when">{v.CanaryTwitch.When}</span>{/if}
          {:else}
            <span class="note">Not configured</span>
          {/if}
        </div>
        <div class="canary-result">
          <strong>Kick</strong>
          {#if v.CanaryKick?.Configured}
            <span class="canary-status {v.CanaryKick.OK ? 'ok' : 'fail'}">{v.CanaryKick.OK ? 'OK' : 'FAIL'}</span>
            <span class="canary-detail">{v.CanaryKick.Detail}</span>
            {#if v.CanaryKick.When}<span class="canary-when">{v.CanaryKick.When}</span>{/if}
          {:else}
            <span class="note">Not configured</span>
          {/if}
        </div>
      </div>
    </section>

    <section class="settings-section">
      <h2>Canary Settings</h2>
      <form class="settings-form" onsubmit={save}>
        <label>Twitch Channel<input bind:value={twitchChannel} placeholder="alveussanctuary" /></label>
        <label>Kick Channel<input bind:value={kickChannel} placeholder="kick" /></label>
        <label>Interval (seconds)<input type="number" bind:value={intervalSec} min="60" /></label>
        <div class="form-actions">
          <button type="submit" disabled={busy}>Save</button>
          <button type="button" onclick={runNow} disabled={running}>Run now</button>
        </div>
      </form>
    </section>
  {:else}
    <p class="loading">Loading…</p>
  {/if}
</div>
