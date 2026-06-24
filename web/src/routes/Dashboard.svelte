<script lang="ts">
  import { onDestroy } from 'svelte';
  import { fetchDashboard } from '../lib/api';
  import { pollingResource, type PollingResource } from '../lib/poll.svelte';
  import type { DashboardSnapshot } from '../lib/types';

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
</script>

{#if display}
  <section class="dash-telemetry">
    <div class="tile"><span class="label">Watch time</span><span class="value">{display.Tele.WatchTimeTotal}</span></div>
    <div class="tile"><span class="label">Claims total</span><span class="value">{display.Tele.ClaimsTotal}</span></div>
    <div class="tile"><span class="label">Active campaigns</span><span class="value">{display.Tele.ActiveCamps}</span></div>
    <div class="tile"><span class="label">Next claim</span><span class="value"><span class="eta">{display.Tele.NextClaimETA}</span> <span class="name">{display.Tele.NextClaimName}</span></span></div>
  </section>

  <section class="mining-columns">
    <div class="col twitch">
      <h3>TWITCH</h3>
      {#each display.Mining.Twitch ?? [] as card (card.ID)}
        <article class="mine-card">
          <span class="name">{card.Name}</span>
          <span class="state">{card.State}</span>
          <span class="channel">{card.Channel}</span>
          <span class="drop">{card.DropName} {card.DropPercent}%</span>
        </article>
      {/each}
    </div>
    <div class="col kick">
      <h3>KICK · {display.Mining.KickWatchMode}</h3>
      {#each display.Mining.Kick ?? [] as card (card.ID)}
        <article class="mine-card">
          <span class="name">{card.Name}</span>
          <span class="state">{card.State}</span>
          <span class="channel">{card.Channel}</span>
          <span class="drop">{card.DropName} {card.DropPercent}%</span>
        </article>
      {/each}
    </div>
  </section>

  <footer class="dash-footer">updated {display.UpdatedAt} · uptime {display.Uptime}</footer>
{:else if error}
  <p class="error">{error.message}</p>
{:else}
  <p class="loading">Loading…</p>
{/if}
