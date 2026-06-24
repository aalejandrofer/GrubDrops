<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { currentPath, startRouter } from './lib/router.svelte';
  import AppShell from './routes/AppShell.svelte';
  import Dashboard from './routes/Dashboard.svelte';
  import Drops from './routes/Drops.svelte';
  import Priority from './routes/Priority.svelte';
  import SettingsShell from './routes/SettingsShell.svelte';
  import SettingsGeneral from './routes/SettingsGeneral.svelte';
  import SettingsNotifications from './routes/SettingsNotifications.svelte';
  import SettingsExperimental from './routes/SettingsExperimental.svelte';
  import SettingsProxy from './routes/SettingsProxy.svelte';

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
  {:else if currentPath() === '/settings/notifications'}
    <SettingsShell active="notifications">{#snippet children()}<SettingsNotifications />{/snippet}</SettingsShell>
  {:else if currentPath() === '/settings/experimental'}
    <SettingsShell active="experimental">{#snippet children()}<SettingsExperimental />{/snippet}</SettingsShell>
  {:else if currentPath() === '/settings/proxy'}
    <SettingsShell active="proxy">{#snippet children()}<SettingsProxy />{/snippet}</SettingsShell>
  {/if}
</AppShell>
