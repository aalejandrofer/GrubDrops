<script lang="ts">
  import type { DashEvent, DashEventAccount } from '../lib/types';
  let { events, accounts }: { events: DashEvent[] | null; accounts: DashEventAccount[] | null } = $props();

  let kind = $state('all');
  let account = $state('');

  const kinds = ['all', 'claim', 'progress', 'state', 'discovery', 'error', 'auth', 'info'];

  const filtered = $derived(
    (events ?? []).filter(
      (e) => (kind === 'all' || e.Kind === kind) && (account === '' || e.Account === account),
    ),
  );
</script>

<section class="section section-wide">
  <section class="drawer">
    <div class="drawer-head">
      <div class="filter" role="group" aria-label="Live events">
        {#each kinds as k}
          <button type="button" class="filter-btn {kind === k ? 'on' : ''}" onclick={() => (kind = k)}>{k}</button>
        {/each}
      </div>
      {#if accounts && accounts.length}
        <select class="ev-account" bind:value={account}>
          <option value="">all accounts</option>
          {#each accounts as a (a.ID)}<option value={a.ID}>{a.Label}{a.Platform ? ' · ' + a.Platform : ''}</option>{/each}
        </select>
      {/if}
    </div>
    <div class="events" id="events-list">
      {#each filtered as ev (ev.ID)}
        <div class="event event-{ev.Color}">
          <span class="ev-time">{ev.Time}</span>
          <span class="ev-body">{@html ev.BodyHTML}</span>
          {#if ev.Account}<span class="ev-acc platform-{ev.Platform}">{ev.Account}</span>{/if}
        </div>
      {/each}
    </div>
  </section>
</section>
