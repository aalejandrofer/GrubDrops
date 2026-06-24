<script lang="ts">
  import type { DashCampaign } from '../lib/types';
  let { camps, onSelect }: { camps: DashCampaign[] | null; onSelect?: (id: string) => void } = $props();
</script>

{#if camps && camps.length}
  <section class="section active-campaigns">
    <h3>Active campaigns</h3>
    {#each camps as c (c.ID)}
      <button type="button" class="campaign-row {c.EndsUrgent ? 'urgent' : ''}" onclick={() => onSelect?.(c.ID)}>
        <span class="camp-name">{c.Name}</span>
        <span class="camp-game">{c.Game}</span>
        <span class="camp-platform platform-{c.Platform}">{c.Platform}</span>
        <span class="camp-claimed">{c.Claimed} / {c.Total}</span>
        <span class="camp-ends">{c.EndsIn}</span>
      </button>
    {/each}
  </section>
{/if}
