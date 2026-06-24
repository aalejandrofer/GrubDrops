<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { currentPath, navigate } from '../lib/router.svelte';
  import { apiFetch, apiSend, ApiError } from '../lib/api';

  const id = currentPath().split('/')[2];

  let userCode = $state('');
  let url = $state('');
  let status = $state('pending');
  let error = $state<string | null>(null);
  let displayName = $state('');
  let intervalId: ReturnType<typeof setInterval> | null = null;

  function clearPoll() {
    if (intervalId !== null) {
      clearInterval(intervalId);
      intervalId = null;
    }
  }

  function startPoll() {
    clearPoll();
    intervalId = setInterval(async () => {
      try {
        const res = await apiFetch<{ status: string }>('/api/accounts/' + id + '/twitch/poll');
        status = res.status;
        if (res.status === 'done') {
          clearPoll();
          navigate('/accounts');
        } else if (res.status === 'expired' || res.status === 'error') {
          clearPoll();
        }
      } catch {
        // polling errors are non-fatal; keep trying
      }
    }, 3000);
  }

  async function start() {
    error = null;
    status = 'pending';
    userCode = '';
    url = '';
    try {
      const res = await apiSend<{ user_code: string; verification_url: string }>(
        '/api/accounts/' + id + '/twitch/device',
        'POST',
      );
      userCode = res.user_code;
      url = res.verification_url;
      startPoll();
    } catch (e) {
      error = e instanceof ApiError ? e.message : 'Failed to start device login';
    }
  }

  onMount(async () => {
    try {
      const acc = await apiFetch<{ Platform: string; DisplayName: string }>('/api/accounts/' + id);
      displayName = acc.DisplayName;
    } catch { /* non-fatal */ }
    await start();
  });

  onDestroy(() => { clearPoll(); });
</script>

<div class="auth-wrap wide">
  <div class="auth-card">
    <div class="kicker">// twitch · device-code</div>
    <h1>Authorize {displayName || id}</h1>
    <p class="intro">Open the URL in a browser logged into the throwaway Twitch account, enter the code, confirm.</p>

    {#if error}
      <div class="err">{error}</div>
    {:else if userCode}
      <div class="device-code">
        <div class="label">user code</div>
        <div class="code">{userCode}</div>
        <a class="url" href={url} target="_blank" rel="noopener">&rarr; {url}</a>
      </div>
    {/if}

    <div style="text-align:center; font-family: 'JetBrains Mono', monospace; font-size: 12px; color: var(--muted); padding: 12px 0;">
      {#if status === 'pending'}
        <em>waiting for authorization…</em>
      {:else if status === 'expired'}
        <em>Code expired.</em>
      {:else if status === 'error'}
        <em>Authorization error.</em>
      {:else if status === 'done'}
        <em>Authorized! Redirecting…</em>
      {/if}
    </div>

    <div class="submit-row">
      <button class="alt" type="button" onclick={() => navigate('/accounts')}>← cancel</button>
      {#if status === 'expired' || status === 'error'}
        <button class="btn primary" type="button" onclick={start}>Retry →</button>
      {:else}
        <span class="alt" style="color: var(--muted);">polling 3s</span>
      {/if}
    </div>
  </div>
</div>
