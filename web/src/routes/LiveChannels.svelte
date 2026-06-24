<script lang="ts">
  import type { DashLiveChannel } from '../lib/types';
  let { channels }: { channels: DashLiveChannel[] | null } = $props();
</script>

{#if channels && channels.length}
  <section class="section live-channels">
    <h3>Live channels</h3>
    <div class="channel-grid">
      {#each channels as ch (ch.Platform + ch.Login)}
        <a class="channel-card" href={ch.URL} target="_blank" rel="noopener">
          <span class="ch-initial">{ch.Initial}</span>
          <span class="ch-login">{ch.Login}</span>
          <span class="ch-game">{ch.Game}</span>
          <span class="ch-views">{ch.Views}</span>
        </a>
      {/each}
    </div>
  </section>
{/if}
