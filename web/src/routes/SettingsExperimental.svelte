<script lang="ts">
  import { onMount } from 'svelte';
  import { apiFetch, apiSend } from '../lib/api';
  import type { SettingsView } from '../lib/types';

  let v = $state<SettingsView | null>(null);
  let kickWatchMode = $state('auto');
  let busy = $state(false);

  async function load() {
    v = await apiFetch<SettingsView>('/api/settings');
    kickWatchMode = v.KickWatchMode;
  }
  onMount(load);

  async function save() {
    busy = true;
    try {
      await apiSend('/api/settings/experimental', 'POST', {
        kick_watch_mode: kickWatchMode,
      });
      await load();
    } finally { busy = false; }
  }
</script>

{#if v}
  <form class="settings-form" onsubmit={(e) => { e.preventDefault(); save(); }}>
    <label>Kick watch mode
      <select bind:value={kickWatchMode}>
        <option value="auto">Auto (recommended)</option>
        <option value="browser">Browser sidecar</option>
        <option value="ws">WebSocket only</option>
      </select>
    </label>
    <div class="form-actions">
      <button type="submit" disabled={busy}>Save</button>
    </div>
  </form>
{:else}
  <p class="loading">Loading…</p>
{/if}
