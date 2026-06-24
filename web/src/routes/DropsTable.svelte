<script lang="ts">
  import { apiSend } from '../lib/api';
  import type { DropRow, DropsAccount } from '../lib/types';

  let {
    rows,
    accounts = null,
    onMutated,
    kind = 'current',
  }: {
    rows: DropRow[] | null;
    accounts?: DropsAccount[] | null;
    onMutated?: () => void;
    kind?: 'current' | 'unlisted' | 'nullgame' | 'past' | 'upcoming';
  } = $props();

  let busy = $state(false);

  // Per-row selected account state (keyed by row index).
  let selectedAccounts = $state<Record<number, string>>({});

  function accountsForPlatform(platform: string): DropsAccount[] {
    return (accounts ?? []).filter((a) => a.Platform === platform);
  }

  async function markLinked(campaignID: string, unlink: boolean) {
    busy = true;
    try {
      await apiSend('/api/drops/link', 'POST', { campaign_id: campaignID, unlink });
      onMutated?.();
    } finally {
      busy = false;
    }
  }

  async function addWhitelist(accountID: string, name: string) {
    if (!accountID) return;
    busy = true;
    try {
      await apiSend('/api/drops/whitelist/add', 'POST', { account_id: accountID, name });
      onMutated?.();
    } finally {
      busy = false;
    }
  }

  async function addChannel(accountID: string, channels: string[]) {
    if (!accountID) return;
    busy = true;
    try {
      await apiSend('/api/drops/whitelist/channel', 'POST', { account_id: accountID, channels });
      onMutated?.();
    } finally {
      busy = false;
    }
  }

  async function removeChannel(accountID: string, channels: string[]) {
    busy = true;
    try {
      await apiSend('/api/drops/whitelist/channel/remove', 'POST', { account_id: accountID, channels });
      onMutated?.();
    } finally {
      busy = false;
    }
  }
</script>

{#if rows && rows.length}
  <table class="drops-table">
    <thead>
      <tr>
        <th>Platform</th><th>Game</th><th>Campaign</th><th>Drop</th><th>Collected</th><th>When</th>
        {#if onMutated}<th>Actions</th>{/if}
      </tr>
    </thead>
    <tbody>
      {#each rows as r, i (r.CampaignID + r.BenefitName + r.AccountName)}
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
          {#if onMutated}
            <td class="actions">
              {#if kind === 'current'}
                {#if r.Linked}
                  <button disabled={busy} onclick={() => markLinked(r.CampaignID, true)}>Unlink</button>
                {:else}
                  <button disabled={busy} onclick={() => markLinked(r.CampaignID, false)}>Mark linked</button>
                {/if}
              {:else if kind === 'unlisted'}
                {@const accts = accountsForPlatform(r.Platform)}
                {#if accts.length}
                  <select
                    value={selectedAccounts[i] ?? accts[0]?.ID ?? ''}
                    onchange={(e) => { selectedAccounts[i] = (e.target as HTMLSelectElement).value; }}
                  >
                    {#each accts as a (a.ID)}
                      <option value={a.ID}>{a.Label}</option>
                    {/each}
                  </select>
                  <button
                    disabled={busy}
                    onclick={() => addWhitelist(selectedAccounts[i] ?? accts[0]?.ID ?? '', r.Game)}
                  >Whitelist+</button>
                {/if}
              {:else if kind === 'nullgame'}
                {#each r.WhitelistedBy ?? [] as chip (chip.AccountID)}
                  <span class="chip">
                    {chip.Login}
                    <button
                      class="chip-remove"
                      disabled={busy}
                      aria-label="Remove {chip.Login}"
                      onclick={() => removeChannel(chip.AccountID, r.Channels ?? [])}
                    >✕</button>
                  </span>
                {/each}
                {@const nullaccts = accountsForPlatform(r.Platform)}
                {#if nullaccts.length}
                  <select
                    value={selectedAccounts[i] ?? nullaccts[0]?.ID ?? ''}
                    onchange={(e) => { selectedAccounts[i] = (e.target as HTMLSelectElement).value; }}
                  >
                    {#each nullaccts as a (a.ID)}
                      <option value={a.ID}>{a.Label}</option>
                    {/each}
                  </select>
                  <button
                    disabled={busy}
                    onclick={() => addChannel(selectedAccounts[i] ?? nullaccts[0]?.ID ?? '', r.Channels ?? [])}
                  >Whitelist+</button>
                {/if}
              {/if}
            </td>
          {/if}
        </tr>
      {/each}
    </tbody>
  </table>
{/if}
