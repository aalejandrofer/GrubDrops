<script lang="ts">
  import type { Snippet } from 'svelte';
  let { active, children }: { active: string; children: Snippet } = $props();
  const tabs = [
    { key: 'general', label: 'General', href: '/settings' },
    { key: 'accounts', label: 'Accounts', href: '/settings/accounts' },
    { key: 'notifications', label: 'Notifications', href: '/settings/notifications' },
    { key: 'security', label: 'Security', href: '/settings/security' },
    { key: 'health', label: 'Health', href: '/settings/health' },
    { key: 'experimental', label: 'Experimental', href: '/settings/experimental' },
    { key: 'proxy', label: 'Proxy', href: '/settings/proxy' },
  ];
</script>

<div class="page-head"><div class="kicker">Settings</div></div>
<nav class="subnav">
  {#each tabs as t (t.key)}
    <a href={t.href} class="subnav-link {active === t.key ? 'on' : ''}">{t.label}</a>
  {/each}
</nav>
<section class="settings-body">
  {@render children()}
</section>
