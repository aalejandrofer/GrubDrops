<script lang="ts">
  import { apiSend } from '../lib/api';
  import { navigate } from '../lib/router.svelte';
  import { ApiError } from '../lib/api';

  let platform = $state('twitch');
  let displayName = $state('');
  let busy = $state(false);
  let error = $state<string | null>(null);

  async function create() {
    if (!displayName.trim()) return;
    busy = true; error = null;
    try {
      const res = await apiSend<{ ok: boolean; id: string }>('/api/accounts/new', 'POST', { platform, display_name: displayName.trim() });
      navigate('/accounts/' + res.id);
    } catch (e) {
      error = e instanceof ApiError ? e.message : 'Failed to create account';
    } finally { busy = false; }
  }
</script>

<div class="page-head"><div class="kicker">Add account</div></div>
<form class="new-account" onsubmit={(e) => { e.preventDefault(); create(); }}>
  <label>Platform
    <select bind:value={platform}>
      <option value="twitch">Twitch</option>
      <option value="kick">Kick</option>
    </select>
  </label>
  <label>Display name
    <input bind:value={displayName} required placeholder="account label" />
  </label>
  {#if error}<p class="error">{error}</p>{/if}
  <button type="submit" disabled={busy || !displayName.trim()}>Create</button>
  <a class="btn" href="/accounts">Cancel</a>
</form>
