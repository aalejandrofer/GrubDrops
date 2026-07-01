<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { currentPath, startRouter } from './lib/router.svelte';
  import AppShell from './routes/AppShell.svelte';
  import Dashboard from './routes/Dashboard.svelte';

  let teardown: (() => void) | undefined;
  onMount(() => { teardown = startRouter(); });
  onDestroy(() => teardown?.());
</script>

<AppShell>
  {#if currentPath() === '/'}
    <Dashboard />
  {/if}
</AppShell>
