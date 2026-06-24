<script lang="ts">
  import { onMount } from 'svelte';
  import { apiFetch, apiSend } from '../lib/api';
  import type { AccountDetail } from '../lib/types';

  let { accountId, onClose }: { accountId: string; onClose: () => void } = $props();

  let detail = $state<AccountDetail | null>(null);
  let busy = $state(false);

  async function load() {
    detail = await apiFetch<AccountDetail>(`/api/dashboard/account/${accountId}`);
  }
  onMount(load);

  async function toggle() {
    busy = true;
    try { await apiSend(`/api/accounts/${accountId}/toggle`, 'POST'); await load(); }
    finally { busy = false; }
  }

  async function reload() {
    busy = true;
    try { await apiSend(`/api/accounts/${accountId}/reload`, 'POST'); await load(); }
    finally { busy = false; }
  }

  async function forceWatch() {
    busy = true;
    try { await apiSend(`/api/accounts/${accountId}/force-watch`, 'POST', { enabled: true }); await load(); }
    finally { busy = false; }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') onClose();
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
<div
  class="modal-overlay"
  onclick={(e) => { if (e.target === e.currentTarget) onClose(); }}
  role="presentation"
>
  <div
    class="modal account-modal"
    role="dialog"
    aria-modal="true"
    aria-labelledby="account-modal-title"
  >
    {#if detail}
      <h2 id="account-modal-title" class="modal-title">{detail.DisplayName}</h2>
      <p class="state">{detail.StateLabel} · {detail.CurrentChannel}</p>

      {#if detail.EligibleCampaigns && detail.EligibleCampaigns.length > 0}
        <section class="modal-campaigns">
          <h3>Eligible campaigns</h3>
          <ul>
            {#each detail.EligibleCampaigns as c (c.ID)}
              <li>{c.Name} — {c.Game} <span class="eta">{c.EndsIn}</span></li>
            {/each}
          </ul>
        </section>
      {/if}

      {#if detail.UpcomingCampaigns && detail.UpcomingCampaigns.length > 0}
        <section class="modal-campaigns modal-campaigns--upcoming">
          <h3>Upcoming campaigns</h3>
          <ul>
            {#each detail.UpcomingCampaigns as c (c.ID)}
              <li>{c.Name} — {c.Game} <span class="eta">{c.StartsIn}</span></li>
            {/each}
          </ul>
        </section>
      {/if}

      <div class="modal-actions">
        <button onclick={toggle} disabled={busy}>
          {detail.Enabled ? 'Disable' : 'Enable'}
        </button>
        <button onclick={reload} disabled={busy}>Reload</button>
        <button onclick={forceWatch} disabled={busy}>Force-watch</button>
      </div>

      <button class="modal-close" aria-label="Close" onclick={onClose}>×</button>
    {:else}
      <p class="loading">Loading…</p>
    {/if}
  </div>
</div>

<style>
  .modal-overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.6);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 200;
  }

  .account-modal {
    background: var(--surface, #1e1e2e);
    border: 1px solid var(--border, #313244);
    border-radius: 8px;
    padding: 1.5rem;
    min-width: 320px;
    max-width: 560px;
    width: 90%;
    max-height: 80vh;
    overflow-y: auto;
    position: relative;
  }

  .modal-title {
    margin: 0 0 0.5rem;
    font-size: 1.1rem;
    font-weight: 600;
  }

  .state {
    margin: 0 0 1rem;
    font-size: 0.875rem;
    color: var(--subtext0, #a6adc8);
  }

  .modal-campaigns {
    margin: 0.75rem 0;
  }

  .modal-campaigns h3 {
    font-size: 0.8rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--subtext1, #bac2de);
    margin: 0 0 0.4rem;
  }

  .modal-campaigns ul {
    list-style: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }

  .modal-campaigns li {
    font-size: 0.85rem;
  }

  .eta {
    color: var(--subtext0, #a6adc8);
    font-size: 0.8em;
  }

  .modal-actions {
    display: flex;
    gap: 0.5rem;
    margin-top: 1.25rem;
    flex-wrap: wrap;
  }

  .modal-actions button {
    flex: 1;
    min-width: 90px;
  }

  .modal-close {
    position: absolute;
    top: 0.75rem;
    right: 0.75rem;
    background: none;
    border: none;
    color: var(--subtext0, #a6adc8);
    font-size: 1.25rem;
    cursor: pointer;
    line-height: 1;
    padding: 0.25rem 0.5rem;
    border-radius: 4px;
  }

  .modal-close:hover {
    color: var(--text, #cdd6f4);
    background: var(--surface1, #313244);
  }

  .loading {
    text-align: center;
    padding: 2rem;
    color: var(--subtext0, #a6adc8);
  }
</style>
