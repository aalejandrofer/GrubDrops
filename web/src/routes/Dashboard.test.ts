import { afterEach, expect, test, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import Dashboard from './Dashboard.svelte';
import type { DashboardSnapshot, AccountDetail } from '../lib/types';

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

test('clicking a mining card opens AccountModal with account detail', async () => {
  const accountDetail: AccountDetail = {
    ID: 'a1', Platform: 'twitch', DisplayName: 'acc-one', Enabled: true,
    State: 'watching', StateLabel: 'Watching', CurrentCampaign: 'Camp', CurrentGame: 'Game',
    CurrentChannel: 'somechan', ProgressPct: 42, WatchETA: '01:00', Uptime: '5m',
    Games: [{ Rank: 1, Name: 'Game' }], EligibleCampaigns: [], UpcomingCampaigns: [],
  };

  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    // AccountModal fetches /api/dashboard/account/a1
    return new Response(JSON.stringify(accountDetail), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    });
  }));

  render(Dashboard, { props: { snapshot: snap(7, 'Wolf Helmet') } });

  // The card should be rendered as a button
  const card = screen.getByRole('button', { name: /open details for acc-one/i });
  await fireEvent.click(card);

  // Modal should open with a dialog role
  const dialog = await screen.findByRole('dialog');
  expect(dialog).toBeTruthy();
  // The modal title (h2) inside the dialog contains the account display name
  const heading = dialog.querySelector('h2');
  expect(heading?.textContent?.trim()).toBe('acc-one');
});
