<script lang="ts">
  import { onMount } from 'svelte';
  import { apiFetch, apiSend } from '../lib/api';
  import type { SettingsView } from '../lib/types';

  let v = $state<SettingsView | null>(null);
  let proxyEnabled = $state(false);
  let proxyUrl = $state('');
  let busy = $state(false);
  let testResult = $state('');

  async function load() {
    v = await apiFetch<SettingsView>('/api/settings');
    proxyEnabled = v.ProxyEnabled;
    proxyUrl = v.ProxyURL;
  }
  onMount(load);

  async function save() {
    busy = true;
    try {
      await apiSend('/api/settings/proxy', 'POST', {
        proxy_enabled: proxyEnabled,
        proxy_url: proxyUrl,
      });
      await load();
    } finally { busy = false; }
  }

  async function testProxy() {
    testResult = 'Testing…';
    const res = await apiSend<{ ok: boolean; message?: string }>('/api/settings/proxy/test', 'POST');
    testResult = res?.ok ? 'OK' : (res?.message ?? 'Failed');
    setTimeout(() => { testResult = ''; }, 4000);
  }
</script>

{#if v}
  <form class="settings-form" onsubmit={(e) => { e.preventDefault(); save(); }}>
    <label class="checkbox"><input type="checkbox" bind:checked={proxyEnabled} /> Enable proxy</label>
    <label>Proxy URL<input bind:value={proxyUrl} placeholder="socks5://user:pass@host:port" /></label>
    <div class="form-actions">
      <button type="submit" disabled={busy}>Save</button>
      <button type="button" onclick={testProxy}>Test</button>
      {#if testResult}<span class="note">{testResult}</span>{/if}
    </div>
  </form>
{:else}
  <p class="loading">Loading…</p>
{/if}
