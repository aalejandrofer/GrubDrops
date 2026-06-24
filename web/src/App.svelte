<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { currentPath, startRouter } from './lib/router.svelte';
  import AppShell from './routes/AppShell.svelte';
  import Dashboard from './routes/Dashboard.svelte';
  import Drops from './routes/Drops.svelte';
  import Priority from './routes/Priority.svelte';
  import SettingsShell from './routes/SettingsShell.svelte';
  import SettingsGeneral from './routes/SettingsGeneral.svelte';

  let teardown: (() => void) | undefined;
  onMount(() => { teardown = startRouter(); });
  onDestroy(() => teardown?.());
</script>

<AppShell>
  {#if currentPath() === '/'}
    <Dashboard />
  {:else if currentPath() === '/drops'}
    <Drops />
  {:else if currentPath() === '/priority'}
    <Priority />
  {:else if currentPath() === '/settings'}
    <SettingsShell active="general">{#snippet children()}<SettingsGeneral />{/snippet}</SettingsShell>
  {/if}
</AppShell>
