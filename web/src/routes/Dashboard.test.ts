import { render, screen } from '@testing-library/svelte';
import { expect, test } from 'vitest';
import Dashboard from './Dashboard.svelte';
import type { DashboardSnapshot } from '../lib/types';

const snapshot: DashboardSnapshot = {
  Tele: {
    WatchTimeTotal: '12:34',
    ClaimsTotal: 7,
    ClaimsToday: 2,
    ActiveCamps: 3,
    Completed: 1,
    TotalDrops: 5,
    NextClaimETA: '00:13',
    NextClaimName: 'Wolf Helmet',
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

test('renders telemetry tiles from the snapshot', () => {
  render(Dashboard, { props: { snapshot } });
  expect(screen.getByText('Wolf Helmet')).toBeTruthy();
  expect(screen.getByText('acc-one')).toBeTruthy();
});
