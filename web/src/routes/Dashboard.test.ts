import { afterEach, expect, test, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import Dashboard from './Dashboard.svelte';
import type { DashboardSnapshot } from '../lib/types';

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function snap(claims: number, nextName: string): DashboardSnapshot {
  return {
    Tele: {
      WatchTimeTotal: '12:34',
      ClaimsTotal: claims,
      ClaimsToday: 2,
      ActiveCamps: 3,
      Completed: 1,
      TotalDrops: 5,
      NextClaimETA: '00:13',
      NextClaimName: nextName,
    },
    Mining: {
      Twitch: [
        { ID: 'a1', Name: 'acc-one', Platform: 'twitch', State: 'watching', StateSub: 'live', Channel: 'somechan', DropName: 'Helmet', DropPercent: 42, Enabled: true },
      ],
      Kick: null,
      KickWatchMode: 'browser',
    },
    UpdatedAt: '1.2s ago',
    Uptime: '17h 42m',
  };
}

test('renders an injected snapshot without polling', () => {
  const fetchSpy = vi.fn();
  vi.stubGlobal('fetch', fetchSpy);
  render(Dashboard, { props: { snapshot: snap(7, 'Wolf Helmet') } });
  expect(screen.getByText('Wolf Helmet')).toBeTruthy();
  expect(screen.getByText('acc-one')).toBeTruthy();
  expect(fetchSpy).not.toHaveBeenCalled(); // snapshot prop bypasses polling
});

test('polls and re-renders updated telemetry on the next tick', async () => {
  let n = 0;
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => {
      n++;
      const body = n === 1 ? snap(1, 'First Drop') : snap(2, 'Second Drop');
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }),
  );

  render(Dashboard, { props: { intervalMs: 20 } });

  expect(await screen.findByText('First Drop')).toBeTruthy();
  expect(await screen.findByText('Second Drop', {}, { timeout: 1000 })).toBeTruthy();
});
