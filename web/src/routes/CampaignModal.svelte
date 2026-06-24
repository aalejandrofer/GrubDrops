<script lang="ts">
  import { onMount } from 'svelte';
  import { apiFetch } from '../lib/api';
  import type { CampaignDetail } from '../lib/types';

  let { campaignId, onClose }: { campaignId: string; onClose: () => void } = $props();
  let detail = $state<CampaignDetail | null>(null);
  let error = $state<string | null>(null);

  onMount(async () => {
    try { detail = await apiFetch<CampaignDetail>(`/api/dashboard/campaign/${campaignId}`); }
    catch (e) { error = (e as Error).message; }
  });

  function onKey(e: KeyboardEvent) { if (e.key === 'Escape') onClose(); }
</script>

<svelte:window onkeydown={onKey} />

<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
<div class="modal-overlay" onclick={(e) => { if (e.target === e.currentTarget) onClose(); }} role="presentation">
  <div class="modal campaign-modal" role="dialog" aria-modal="true" aria-labelledby="camp-modal-title">
    {#if error}
      <p class="error">{error}</p>
    {:else if detail}
      <h2 id="camp-modal-title" class="modal-title">{detail.Name}</h2>
      <p class="camp-meta">
        <span class="platform-{detail.Platform}">{detail.Platform}</span> · {detail.Game} · {detail.Status}
      </p>
      <p class="camp-dates">{detail.StartsAt} → {detail.EndsAt} ({detail.EndsIn})</p>
      <h3>Drops</h3>
      <ul class="benefits">
        {#each detail.Benefits ?? [] as b (b.ID)}
          <li><span class="benefit-name">{b.Name}</span> <span class="benefit-mins">— {b.RequiredMinutes}m</span></li>
        {/each}
      </ul>
      {#if detail.EligibleAccounts && detail.EligibleAccounts.length}
        <h3>Eligible accounts</h3>
        <ul class="accounts">{#each detail.EligibleAccounts as a}<li>{a}</li>{/each}</ul>
      {/if}
    {:else}
      <p class="loading">Loading…</p>
    {/if}
    <button class="modal-close" aria-label="Close" onclick={onClose}>×</button>
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

  .campaign-modal {
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

  .camp-meta {
    margin: 0 0 0.5rem;
    font-size: 0.875rem;
    color: var(--subtext0, #a6adc8);
  }

  .camp-dates {
    margin: 0 0 1rem;
    font-size: 0.8rem;
    color: var(--subtext1, #bac2de);
  }

  h3 {
    font-size: 0.8rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--subtext1, #bac2de);
    margin: 0.75rem 0 0.4rem;
  }

  .benefits, .accounts {
    list-style: none;
    padding: 0;
    margin: 0 0 0.75rem;
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }

  .benefits li, .accounts li {
    font-size: 0.85rem;
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

  .error {
    color: var(--red, #f38ba8);
    padding: 1rem 0;
  }
</style>
