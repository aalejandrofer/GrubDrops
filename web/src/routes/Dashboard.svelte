<script lang="ts">
  import { onMount } from 'svelte';
  import { fetchDashboard } from '../lib/api';
  import type { DashboardSnapshot } from '../lib/types';

  let { snapshot = null }: { snapshot?: DashboardSnapshot | null } = $props();
  let fetched = $state<DashboardSnapshot | null>(null);
  let error = $state<string | null>(null);

  const display = $derived(snapshot ?? fetched);

  onMount(async () => {
    if (snapshot) return; // test-injected
    try {
      fetched = await fetchDashboard();
    } catch (e) {
      error = (e as Error).message;
    }
  });
</script>

{#if error}
  <p class="error">{error}</p>
{:else if display}
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
{:else}
  <p class="loading">Loading…</p>
{/if}
