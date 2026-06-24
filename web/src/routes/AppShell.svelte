<script lang="ts">
  import type { Snippet } from 'svelte';
  import { currentPath } from '../lib/router.svelte';
  import { readCookie } from '../lib/csrf';

  let { children }: { children: Snippet } = $props();

  const links = [
    { href: '/', label: 'Console' },
    { href: '/drops', label: 'Drops' },
    { href: '/priority', label: 'Priority' },
    { href: '/history', label: 'History' },
    { href: '/settings', label: 'Settings' },
  ];

  function active(href: string): boolean {
    return href === '/' ? currentPath() === '/' : currentPath().startsWith(href);
  }
</script>

<nav class="nav">
  <a class="brand" href="/">GrubDrops</a>
  <div class="nav-links">
    {#each links as l (l.href)}
      <a href={l.href} class={active(l.href) ? 'active' : ''}>{l.label}</a>
    {/each}
  </div>
  <div class="nav-right">
    <form method="post" action="/api/lang" class="lang-form">
      <input type="hidden" name="csrf_token" value={readCookie('csrftoken') ?? ''} />
      <select name="lang" onchange={(e) => (e.currentTarget.form as HTMLFormElement).submit()}>
        <option value="en">EN</option>
        <option value="es">ES</option>
        <option value="zh-CN">中文</option>
      </select>
    </form>
    <a class="gh-link" href="https://github.com/aalejandrofer/grubdrops" target="_blank" rel="noopener" aria-label="GitHub">GitHub</a>
  </div>
</nav>

<main class="app-main">
  {@render children()}
</main>
