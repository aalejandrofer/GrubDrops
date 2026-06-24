<script lang="ts">
  import { onDestroy } from 'svelte';
  import { fetchDashboard } from '../lib/api';
  import { pollingResource, type PollingResource } from '../lib/poll.svelte';
  import type { DashboardSnapshot } from '../lib/types';
  import AccountModal from './AccountModal.svelte';
  import AlertsBanner from './AlertsBanner.svelte';
  import NextClaims from './NextClaims.svelte';
  import ActiveCampaigns from './ActiveCampaigns.svelte';
  import EventsDrawer from './EventsDrawer.svelte';
  import LiveChannels from './LiveChannels.svelte';

  let {
    snapshot = null,
    intervalMs = 10000,
  }: { snapshot?: DashboardSnapshot | null; intervalMs?: number } = $props();

  // An injected snapshot (tests) renders statically and never polls.
  const poll: PollingResource<DashboardSnapshot> | null = snapshot
    ? null
    : pollingResource(fetchDashboard, intervalMs);
  onDestroy(() => poll?.stop());

  // Last good snapshot wins over a transient poll error (no blanking);
  // the error state only shows on a first-load failure with no data yet.
  const display = $derived(snapshot ?? poll?.current ?? null);
  const error = $derived(snapshot ? null : poll?.error ?? null);

  let selected = $state<string | null>(null);
</script>

{#if display}
  <div class="page-head">
    <div><div class="kicker">Dashboard</div><div class="sub">uptime {display.Uptime}</div></div>
  </div>

  <AlertsBanner alerts={display.Alerts} />

  <section class="dash-telemetry">
    <div class="tile"><span class="label">Watch time</span><span class="value">{display.Tele.WatchTimeTotal}</span></div>
    <div class="tile"><span class="label">Claims total</span><span class="value">{display.Tele.ClaimsTotal}</span></div>
    <div class="tile"><span class="label">Active campaigns</span><span class="value">{display.Tele.ActiveCamps}</span></div>
    <div class="tile"><span class="label">Next claim</span><span class="value"><span class="eta">{display.Tele.NextClaimETA}</span> <span class="name">{display.Tele.NextClaimName}</span></span></div>
  </section>

  <NextClaims claims={display.NextClaims} />

  <section class="mining-columns">
    <div class="col twitch">
      <h3>TWITCH</h3>
      {#each display.Mining.Twitch ?? [] as card (card.ID)}
        <button
          class="mine-card"
          onclick={() => (selected = card.ID)}
          aria-label="Open details for {card.Name}"
        >
          <span class="name">{card.Name}</span>
          <span class="state">{card.State}</span>
          <span class="channel">{card.Channel}</span>
          <span class="drop">{card.DropName} {card.DropPercent}%</span>
        </button>
      {/each}
    </div>
    <div class="col kick">
      <h3>KICK · {display.Mining.KickWatchMode}</h3>
      {#each display.Mining.Kick ?? [] as card (card.ID)}
        <button
          class="mine-card"
          onclick={() => (selected = card.ID)}
          aria-label="Open details for {card.Name}"
        >
          <span class="name">{card.Name}</span>
          <span class="state">{card.State}</span>
          <span class="channel">{card.Channel}</span>
          <span class="drop">{card.DropName} {card.DropPercent}%</span>
        </button>
      {/each}
    </div>
  </section>

  <ActiveCampaigns camps={display.ActiveCamps} />
  <EventsDrawer events={display.Events} accounts={display.EventAccounts} />
  <LiveChannels channels={display.LiveChannels} />

  <footer class="dash-footer">updated {display.UpdatedAt} · uptime {display.Uptime}</footer>

  {#if selected}
    <AccountModal accountId={selected} onClose={() => (selected = null)} />
  {/if}
{:else if error}
  <p class="error">{error.message}</p>
{:else}
  <p class="loading">Loading…</p>
{/if}
