<script lang="ts">
  import { currentPath, navigate } from '../lib/router.svelte';
  import { fetchDrops } from '../lib/api';
  import DropsTable from './DropsTable.svelte';
  import type { DropsPage } from '../lib/types';

  const tabs = [
    { key: 'current', label: 'Current' },
    { key: 'past', label: 'Past' },
    { key: 'upcoming', label: 'Upcoming' },
  ];

  // Active tab from the URL search (router preserves it).
  function activeTab(): string {
    const t = new URLSearchParams(location.search).get('tab') ?? 'current';
    return tabs.some((x) => x.key === t) ? t : 'current';
  }

  let page = $state<DropsPage | null>(null);
  let error = $state<string | null>(null);
  let loadedTab = $state('');

  // Refetch whenever the active tab changes. currentPath() is reactive, so
  // this effect re-runs on navigation; we guard against refetching the same tab.
  $effect(() => {
    void currentPath(); // track navigation
    const t = activeTab();
    if (t === loadedTab) return;
    loadedTab = t;
    error = null;
    fetchDrops(t).then((p) => (page = p)).catch((e) => (error = (e as Error).message));
  });
</script>

<div class="page-head"><div class="kicker">Drops</div></div>

<div class="tabs">
  {#each tabs as t (t.key)}
    <a href={'/drops?tab=' + t.key} class={activeTab() === t.key ? 'tab on' : 'tab'}>{t.label}</a>
  {/each}
</div>

{#if error}
  <p class="error">{error}</p>
{:else if page}
  {#if page.NoWhitelist}
    <p class="cold-start">No games whitelisted yet — add a game to start discovering drops.</p>
  {:else}
    <DropsTable rows={page.Rows} />
    {#if page.UnlinkedRows && page.UnlinkedRows.length}
      <h3>Not linked</h3>
      <DropsTable rows={page.UnlinkedRows} />
    {/if}
    {#if page.NullGameRows && page.NullGameRows.length}
      <h3>Channel drops</h3>
      <DropsTable rows={page.NullGameRows} />
    {/if}
    {#if page.UnlistedRows && page.UnlistedRows.length}
      <h3>Discoverable</h3>
      <DropsTable rows={page.UnlistedRows} />
    {/if}
  {/if}
{:else}
  <p class="loading">Loading…</p>
{/if}
