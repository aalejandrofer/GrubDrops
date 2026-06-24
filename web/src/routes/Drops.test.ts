import { afterEach, expect, test, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import Drops from './Drops.svelte';
import type { DropsPage } from '../lib/types';

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

function page(tab: string): DropsPage {
  return {
    Tab: tab, PastCount: 1, CurrentCount: 2, UpcomingCount: 0,
    Rows: [{
      CampaignID: 'c1', When: 'w', Platform: 'twitch', Game: 'GameX',
      CampaignName: 'Camp ' + tab, BenefitName: 'Helmet', AccountName: 'a', Kind: 'drop',
      ActionOnly: false, Collectors: null, Channels: null, WhitelistedBy: null,
      Linked: true, LinkURL: '', ConnectChips: null, NeedsConnect: false,
    }],
    UnlinkedRows: null, UnlistedRows: null, NullGameRows: null, Accounts: null, NoWhitelist: false,
  };
}

test('fetches the current tab and renders its rows', async () => {
  history.replaceState({}, '', '/drops');
  vi.stubGlobal('fetch', vi.fn(async () =>
    new Response(JSON.stringify(page('current')), { status: 200, headers: { 'Content-Type': 'application/json' } }),
  ));
  render(Drops);
  expect(await screen.findByText('Camp current')).toBeTruthy();
  // tab bar present
  expect(screen.getAllByText(/current/i).length).toBeGreaterThan(0);
});

test('shows the cold-start CTA when NoWhitelist', async () => {
  history.replaceState({}, '', '/drops');
  const p = page('current'); p.NoWhitelist = true; p.Rows = null;
  vi.stubGlobal('fetch', vi.fn(async () =>
    new Response(JSON.stringify(p), { status: 200, headers: { 'Content-Type': 'application/json' } }),
  ));
  render(Drops);
  expect(await screen.findByText(/no games whitelisted|whitelist a game|add a game/i)).toBeTruthy();
});
