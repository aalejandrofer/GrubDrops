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
  let busy = $state(false);

  // edit form state (initialised from fetched data)
  let displayName = $state('');
  let webhookURL = $state('');
  let enabled = $state(false);

  // add-input states
  let newGameName = $state('');
  let newChannel = $state('');
  let newForceChannel = $state('');

  async function load() {
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

  // Keep fetchDetail as alias for backward compat with existing code paths
  async function fetchDetail() { await load(); }

  onMount(() => { load(); });

  async function act(path: string, body: unknown = undefined) {
    busy = true;
    try {
      await apiSend('/api/accounts/' + id + path, 'POST', body);
      await load();
    } finally {
      busy = false;
    }
  }

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

  // Length helpers (used in template to avoid ?? in Svelte expressions)
  function selectedGamesLen(): number { return data?.SelectedGames?.length ?? 0; }
  function forceChannelsLen(): number { return data?.ForceChannels?.length ?? 0; }

  // Games helpers
  function gamesReorder(fromIdx: number, toIdx: number) {
    if (!data?.SelectedGames) return;
    const arr = [...data.SelectedGames];
    const [item] = arr.splice(fromIdx, 1);
    arr.splice(toIdx, 0, item);
    act('/games', { game_ids: arr.map(g => g.ID) });
  }

  function gamesRemove(gameId: string) {
    if (!data?.SelectedGames) return;
    const ids = data.SelectedGames.filter(g => g.ID !== gameId).map(g => g.ID);
    act('/games', { game_ids: ids });
  }

  async function gamesAdd() {
    const name = newGameName.trim();
    if (!name) return;
    newGameName = '';
    await act('/games/add', { name });
  }

  // Channels helpers
  function channelRemove(channel: string) {
    act('/channels/remove', { channel });
  }

  async function channelAdd() {
    const channel = newChannel.trim();
    if (!channel) return;
    newChannel = '';
    await act('/channels/add', { channel });
  }

  // Force-channels helpers
  function forceChannelReorder(fromIdx: number, toIdx: number) {
    if (!data?.ForceChannels) return;
    const arr = [...data.ForceChannels];
    const [item] = arr.splice(fromIdx, 1);
    arr.splice(toIdx, 0, item);
    act('/force-channels', { channels: arr });
  }

  function forceChannelRemove(channel: string) {
    act('/force-channels/remove', { channel });
  }

  async function forceChannelAdd() {
    const channel = newForceChannel.trim();
    if (!channel) return;
    newForceChannel = '';
    await act('/force-channels/add', { channel });
  }

  function toggleForceWatch() {
    const current = data?.ForceWatchEnabled ?? false;
    act('/force-watch', { enabled: !current });
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
          {#each data.SelectedGames as g, i (g.ID)}
            <li>
              <button type="button" disabled={busy || i === 0} onclick={() => gamesReorder(i, i - 1)}>▲</button>
              <button type="button" disabled={busy || i >= selectedGamesLen() - 1} onclick={() => gamesReorder(i, i + 1)}>▼</button>
              <span>{g.Name} (rank {g.Rank})</span>
              <button type="button" disabled={busy} onclick={() => gamesRemove(g.ID)}>✕</button>
            </li>
          {/each}
        </ul>
      {:else}
        <p>No games selected.</p>
      {/if}
      <div>
        <input type="text" placeholder="Add game" bind:value={newGameName} disabled={busy} />
        <button type="button" disabled={busy || !newGameName.trim()} onclick={gamesAdd}>Add Game</button>
      </div>
      <button type="button" disabled={busy} onclick={() => act('/games/use-global')}>Use global whitelist</button>
    </section>

    <section class="detail-section">
      <h2>Channels</h2>
      {#if data.Channels && data.Channels.length > 0}
        <ul>
          {#each data.Channels as ch}
            <li>
              <span>{ch}</span>
              <button type="button" disabled={busy} onclick={() => channelRemove(ch)}>✕</button>
            </li>
          {/each}
        </ul>
      {:else}
        <p>No channels.</p>
      {/if}
      <div>
        <input type="text" placeholder="Add channel" bind:value={newChannel} disabled={busy} />
        <button type="button" disabled={busy || !newChannel.trim()} onclick={channelAdd}>Add Channel</button>
      </div>
    </section>

    <section class="detail-section">
      <h2>Force Channels</h2>
      {#if data.ForceChannels && data.ForceChannels.length > 0}
        <ul>
          {#each data.ForceChannels as ch, i (ch)}
            <li>
              <button type="button" disabled={busy || i === 0} onclick={() => forceChannelReorder(i, i - 1)}>▲</button>
              <button type="button" disabled={busy || i >= forceChannelsLen() - 1} onclick={() => forceChannelReorder(i, i + 1)}>▼</button>
              <span>{ch}</span>
              <button type="button" disabled={busy} onclick={() => forceChannelRemove(ch)}>✕</button>
            </li>
          {/each}
        </ul>
      {:else}
        <p>No force channels.</p>
      {/if}
      <div>
        <input type="text" placeholder="Add force channel" bind:value={newForceChannel} disabled={busy} />
        <button type="button" disabled={busy || !newForceChannel.trim()} onclick={forceChannelAdd}>Add Force Channel</button>
      </div>
    </section>

    <section class="detail-section">
      <h2>Force Watch</h2>
      <p>{data.ForceWatchEnabled ? 'Enabled' : 'Disabled'}</p>
      <button type="button" disabled={busy} onclick={toggleForceWatch}>
        {data.ForceWatchEnabled ? 'Disable Force Watch' : 'Enable Force Watch'}
      </button>
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
