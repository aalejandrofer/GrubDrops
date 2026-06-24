<script lang="ts">
  import { onMount } from 'svelte';
  import { apiFetch, apiSend } from '../lib/api';
  import type { SettingsView } from '../lib/types';

  let v = $state<SettingsView | null>(null);
  let retention = $state(0);
  let level = $state('');
  let tick = $state(0);
  let disc = $state(0);
  let busy = $state(false);
  let reloadNote = $state(false);

  async function load() {
    v = await apiFetch<SettingsView>('/api/settings');
    retention = v.LogRetentionDays; level = v.LogLevel; tick = v.TickIntervalSec; disc = v.DiscoveryIntervalMin;
  }
  onMount(load);

  async function save() {
    busy = true;
    try {
      const res = await apiSend<{ ok: boolean; intervals_changed: boolean }>('/api/settings/general', 'POST', {
        log_retention_days: retention, log_level: level, tick_interval_sec: tick, discovery_interval_min: disc,
      });
      reloadNote = !!res?.intervals_changed;
      await load();
    } finally { busy = false; }
  }
</script>

{#if v}
  <form class="settings-form" onsubmit={(e) => { e.preventDefault(); save(); }}>
    <label>Tick interval (s)<input type="number" bind:value={tick} min="1" /></label>
    <label>Discovery interval (min)<input type="number" bind:value={disc} min="1" /></label>
    <label>Log level<input bind:value={level} placeholder={v.LogLevelEnv} /></label>
    <label>Log retention (days)<input type="number" bind:value={retention} min="0" /></label>
    <button type="submit" disabled={busy}>Save</button>
    {#if reloadNote}<span class="note">Interval change applies on next reload.</span>{/if}
  </form>

  <div class="diagnostics">
    <div>Version <b>{v.Version}</b> ({v.GitCommit})</div>
    <div>{v.GoVersion} · {v.Goroutines} goroutines · uptime {v.Uptime}</div>
    {#if v.Sidecars && v.Sidecars.length}<div>Sidecars: {v.Sidecars.join(', ')}</div>{/if}
  </div>
{:else}
  <p class="loading">Loading…</p>
{/if}
