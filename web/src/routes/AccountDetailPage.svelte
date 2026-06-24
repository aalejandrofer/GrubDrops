<script lang="ts">
  import { onMount } from 'svelte';
  import { currentPath, navigate } from '../lib/router.svelte';
  import { apiFetch, apiSend } from '../lib/api';
  import type { AccountDetailPageData } from '../lib/types';

  const id = currentPath().split('/')[2];

  let data: AccountDetailPageData | null = $state(null);
  let loading = $state(true);
  let error = $state('');
  let saving = $state(false);
  let saveErr = $state('');

  // edit form state (initialised from fetched data)
  let displayName = $state('');
  let webhookURL = $state('');
  let enabled = $state(false);

  async function fetchDetail() {
    loading = true;
    error = '';
    try {
      const res = await apiFetch<AccountDetailPageData>('/api/accounts/' + id);
      data = res;
      displayName = res.DisplayName;
      webhookURL = res.WebhookURL;
      enabled = res.Enabled;
    } catch (err: unknown) {
      error = err instanceof Error ? err.message : 'Failed to load account';
    }
    loading = false;
  }

  onMount(() => { fetchDetail(); });

  async function handleSave(e: Event) {
    e.preventDefault();
    saving = true;
    saveErr = '';
    try {
      await apiSend('/api/accounts/' + id + '/update', 'POST', {
        display_name: displayName,
        webhook_url: webhookURL,
        enabled,
      });
      await fetchDetail();
    } catch (err: unknown) {
      saveErr = err instanceof Error ? err.message : 'Save failed';
    }
    saving = false;
  }

  async function handleDelete() {
    if (!window.confirm('Delete this account?')) return;
    try {
      await apiSend('/api/accounts/' + id + '/delete', 'POST');
    } catch {
      // best-effort delete; navigate anyway
    }
    navigate('/accounts');
  }
</script>

{#if loading}
  <p>Loading...</p>
{:else if error}
  <p class="error">{error}</p>
{:else if data}
  <div class="account-detail">
    <header class="account-header">
      {#if data.AvatarURL}
        <img src={data.AvatarURL} alt="avatar" class="avatar" />
      {/if}
      <div>
        <h1>{data.DisplayName}</h1>
        <span class="platform">{data.Platform}</span>
        <span class="status">{data.Status}</span>
      </div>
    </header>

    <form onsubmit={handleSave} class="edit-form">
      <label>
        Display Name
        <input type="text" bind:value={displayName} />
      </label>
      <label>
        Webhook URL
        <input type="text" bind:value={webhookURL} />
      </label>
      <label>
        <input type="checkbox" bind:checked={enabled} /> Enabled
      </label>
      {#if saveErr}<p class="save-err">{saveErr}</p>{/if}
      <button type="submit" disabled={saving}>Save</button>
    </form>

    <section class="detail-section">
      <h2>Selected Games</h2>
      {#if data.SelectedGames && data.SelectedGames.length > 0}
        <ul>
          {#each data.SelectedGames as g}
            <li>{g.Name} (rank {g.Rank})</li>
          {/each}
        </ul>
      {:else}
        <p>No games selected.</p>
      {/if}
    </section>

    <section class="detail-section">
      <h2>Channels</h2>
      {#if data.Channels && data.Channels.length > 0}
        <ul>{#each data.Channels as ch}<li>{ch}</li>{/each}</ul>
      {:else}
        <p>No channels.</p>
      {/if}
    </section>

    <section class="detail-section">
      <h2>Force Channels</h2>
      {#if data.ForceChannels && data.ForceChannels.length > 0}
        <ul>{#each data.ForceChannels as ch}<li>{ch}</li>{/each}</ul>
      {:else}
        <p>No force channels.</p>
      {/if}
    </section>

    <section class="detail-section">
      <h2>Force Watch</h2>
      <p>{data.ForceWatchEnabled ? 'Enabled' : 'Disabled'}</p>
    </section>

    <section class="detail-section">
      <!-- re-auth: /accounts/{id}/login is NOT SPA-owned → full-nav via plain anchor tag (not navigate()) -->
      <a href={'/accounts/' + id + '/login'}>Login / re-auth</a>
    </section>

    <section class="danger-zone">
      <button type="button" onclick={handleDelete} class="delete-btn">Delete Account</button>
    </section>
  </div>
{/if}
