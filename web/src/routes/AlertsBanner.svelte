<script lang="ts">
  import type { DashAlert } from '../lib/types';
  let { alerts }: { alerts: DashAlert[] | null } = $props();
</script>

{#if alerts && alerts.length}
  <section class="alerts">
    {#each alerts as a (a.Kind + a.Account + a.URL)}
      <div class="alert alert-{a.Kind}">
        <span class="alert-tag">{a.Kind}</span>
        <span class="alert-body">{a.Account}</span>
        <a class="btn sm alert-cta" href={a.URL}>{a.Action}</a>
      </div>
    {/each}
  </section>
{/if}
