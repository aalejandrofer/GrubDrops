<script lang="ts">
  import { onMount } from 'svelte';
  import { currentPath, navigate } from '../lib/router.svelte';
  import { apiFetch, apiUpload, ApiError } from '../lib/api';

  const id = currentPath().split('/')[2];

  let displayName = $state('');
  let busy = $state(false);
  let error = $state<string | null>(null);
  let fileInput = $state<HTMLInputElement | null>(null);

  onMount(async () => {
    try {
      const acc = await apiFetch<{ Platform: string; DisplayName: string }>('/api/accounts/' + id);
      displayName = acc.DisplayName;
    } catch { /* non-fatal */ }
  });

  async function submit() {
    error = null;
    if (!fileInput?.files?.length) {
      error = 'No file selected';
      return;
    }
    busy = true;
    try {
      const fd = new FormData();
      fd.append('cookie_file', fileInput.files[0]);
      await apiUpload<{ ok: boolean; verified: boolean }>('/api/accounts/' + id + '/twitch/cookie', fd);
      navigate('/accounts');
    } catch (e) {
      error = e instanceof ApiError ? e.message : 'Import failed';
    } finally {
      busy = false;
    }
  }
</script>

<div class="auth-wrap wide">
  <div class="auth-card">
    <div class="kicker">// twitch · migrate</div>
    <h1>Import cookies for {displayName || id}</h1>
    <p class="intro">Already mining on TwitchDropsMiner? Upload your existing cookies.jar from
      <a href="https://github.com/DevilXD/TwitchDropsMiner" target="_blank" rel="noopener noreferrer">DevilXD</a>
      or
      <a href="https://github.com/rangermix/TwitchDropsMiner" target="_blank" rel="noopener noreferrer">rangermix</a>.</p>
  </div>

  <div class="auth-card">
    <form onsubmit={(e) => { e.preventDefault(); submit(); }}>
      <div class="method-head">
        <h2>Upload cookies.jar</h2>
      </div>

      {#if error}<div class="err">{error}</div>{/if}

      <ol class="steps">
        <li>Find cookies.jar next to your TwitchDropsMiner app (same folder as the executable).</li>
        <li>Upload it below and import.</li>
      </ol>

      <label>cookies.jar <span class="req">(required)</span>
        <input type="file" name="cookie_file" accept=".jar,.pkl,.pickle" required bind:this={fileInput} />
      </label>

      <div class="submit-row">
        <button class="alt" type="button" onclick={() => navigate('/accounts')}>cancel</button>
        <button class="btn primary" type="submit" disabled={busy}>Import →</button>
      </div>
    </form>
  </div>
</div>
