<script lang="ts">
  import { onMount } from 'svelte';
  import { apiFetch, apiSend } from '../lib/api';
  import type { SettingsView, SettingsGameRow } from '../lib/types';

  let view = $state<SettingsView | null>(null);
  let games = $state<SettingsGameRow[]>([]);
  let mode = $state('ordered');
  let busy = $state(false);

  async function load() {
    view = await apiFetch<SettingsView>('/api/settings');
    games = (view.GlobalGames ?? []).slice();
    mode = view.PriorityMode || 'ordered';
  }
  onMount(load);

  async function saveOrder() {
    busy = true;
    try { await apiSend('/api/settings/global-games', 'POST', { game_ids: games.map((g) => g.ID) }); await load(); }
    finally { busy = false; }
  }
  async function move(i: number, delta: number) {
    const j = i + delta;
    if (j < 0 || j >= games.length) return;
    const next = games.slice();
    [next[i], next[j]] = [next[j], next[i]];
    games = next;
    await saveOrder();
  }
  async function setMode(m: string) {
    busy = true;
    try { await apiSend('/api/settings/priority-mode', 'POST', { priority_mode: m }); await load(); }
    finally { busy = false; }
  }
  let addName = $state('');
  async function addGame() {
    if (!addName.trim()) return;
    busy = true;
    try { await apiSend('/api/settings/global-games/add', 'POST', { name: addName.trim() }); addName = ''; await load(); }
    finally { busy = false; }
  }
</script>

<div class="page-head"><div class="kicker">Drop priority</div></div>

{#if view}
  <label class="mode">Mode
    <select value={mode} onchange={(e) => setMode(e.currentTarget.value)} disabled={busy}>
      <option value="ordered">Ordered (priority)</option>
      <option value="ending_soonest">Ending soonest</option>
    </select>
  </label>

  <ol class="priority-list">
    {#each games as g, i (g.ID)}
      <li class="priority-row">
        <span class="rank">{i + 1}</span>
        <span class="name">{g.Name}</span>
        <button type="button" aria-label="Move up ▲" onclick={() => move(i, -1)} disabled={busy || i === 0}>▲</button>
        <button type="button" aria-label="Move down ▼" onclick={() => move(i, 1)} disabled={busy || i === games.length - 1}>▼</button>
      </li>
    {/each}
  </ol>

  <form class="add-game" onsubmit={(e) => { e.preventDefault(); addGame(); }}>
    <input placeholder="Add a game…" bind:value={addName} />
    <button type="submit" disabled={busy || !addName.trim()}>Add</button>
  </form>
{:else}
  <p class="loading">Loading…</p>
{/if}
