<script lang="ts">
  import type { MineCard } from '../lib/types';
  let { claims }: { claims: MineCard[] | null } = $props();
</script>

{#if claims && claims.length}
  <section class="next-claims">
    {#each claims as c (c.ID)}
      <div class="next-claim">
        <span class="acc">{c.Name}</span>
        <span class="drop">{c.DropName}</span>
        <span class="eta">{c.DropETA}</span>
        <span class="pct">{c.DropPercent}%</span>
      </div>
    {/each}
  </section>
{/if}
