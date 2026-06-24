import { expect, test, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import App from './App.svelte';

test('renders the dashboard inside the shell at /', () => {
  history.replaceState({}, '', '/');
  // Dashboard fetches on mount; stub fetch to a minimal valid snapshot.
  vi.stubGlobal('fetch', vi.fn(async () =>
    new Response(JSON.stringify({
      Tele: { WatchTimeTotal: '', ClaimsTotal: 0, ClaimsToday: 0, ActiveCamps: 0, Completed: 0, TotalDrops: 0, NextClaimETA: '', NextClaimName: '' },
      Mining: { Twitch: null, Kick: null, KickWatchMode: '' },
      NextClaims: null, ActiveCamps: null, LiveChannels: null, Events: null, EventAccounts: null, Alerts: null,
      UpdatedAt: '', Uptime: '1h',
    }), { status: 200, headers: { 'Content-Type': 'application/json' } }),
  ));
  render(App);
  // nav present (shell)
  expect(screen.getAllByText(/drops/i).length).toBeGreaterThan(0);
});
