<script lang="ts">
  import type { DropRow } from '../lib/types';
  let { rows }: { rows: DropRow[] | null } = $props();
</script>

{#if rows && rows.length}
  <table class="drops-table">
    <thead>
      <tr><th>Platform</th><th>Game</th><th>Campaign</th><th>Drop</th><th>Collected</th><th>When</th></tr>
    </thead>
    <tbody>
      {#each rows as r (r.CampaignID + r.BenefitName + r.AccountName)}
        <tr class="drop-row">
          <td class="platform-{r.Platform}">{r.Platform}</td>
          <td>{r.Game}</td>
          <td>{r.CampaignName}</td>
          <td>{r.BenefitName}</td>
          <td class="collectors">
            {#if r.ActionOnly}
              <span class="mark cross" title="No watch-time benefit">✕</span>
            {:else}
              {#each r.Collectors ?? [] as c (c.Login)}
                <span class="mark {c.Full ? 'full' : 'partial'} platform-{c.Platform}" title={c.Login}>{c.Login}</span>
              {/each}
            {/if}
          </td>
          <td class="when">{r.When}</td>
        </tr>
      {/each}
    </tbody>
  </table>
{/if}
