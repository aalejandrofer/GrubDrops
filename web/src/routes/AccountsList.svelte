<script lang="ts">
  import { onMount } from 'svelte';
  import { apiFetch, apiSend } from '../lib/api';
  import type { AccountListRow } from '../lib/types';

  let rows = $state<AccountListRow[]>([]);
  let busy = $state(false);

  async function load() { rows = await apiFetch<AccountListRow[]>('/api/accounts'); }
  onMount(load);

  async function toggle(id: string) { busy = true; try { await apiSend(`/api/accounts/${id}/toggle`, 'POST'); await load(); } finally { busy = false; } }
  async function reload(id: string) { busy = true; try { await apiSend(`/api/accounts/${id}/reload`, 'POST'); await load(); } finally { busy = false; } }
  async function checkAuth() { busy = true; try { await apiSend('/api/accounts/check-auth', 'POST'); await load(); } finally { busy = false; } }
</script>

<div class="page-head">
  <div class="kicker">Accounts</div>
  <div class="actions">
    <button type="button" class="btn" onclick={checkAuth} disabled={busy}>Check auth</button>
    <a class="btn primary" href="/accounts/new">Add account</a>
  </div>
</div>

<div class="accounts-list">
  {#each rows as a (a.ID)}
    <div class="account-row state-{a.StateTier}">
      <span class="avatar">{#if a.AvatarURL}<img src={a.AvatarURL} alt="" />{:else}{a.AccountInitial}{/if}</span>
      <a class="name" href={'/accounts/' + a.ID}>{a.DisplayName}</a>
      <span class="platform platform-{a.Platform}">{a.Platform}</span>
      <span class="state pill-{a.StateTier}">{a.State}</span>
      {#if a.AuthChecked}<span class="auth {a.AuthOK ? 'ok' : 'bad'}" title={a.AuthMsg}>{a.AuthOK ? 'auth ok' : 'auth ✗'}</span>{/if}
      <button type="button" onclick={() => toggle(a.ID)} disabled={busy}>{a.Enabled ? 'Disable' : 'Enable'}</button>
      <button type="button" onclick={() => reload(a.ID)} disabled={busy}>Reload</button>
    </div>
  {/each}
</div>
