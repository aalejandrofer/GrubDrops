import { afterEach, expect, test, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
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

test('mutation on current rows triggers a refetch (second GET to /api/drops)', async () => {
  history.replaceState({}, '', '/drops');
  Object.defineProperty(document, 'cookie', { value: 'csrftoken=t', configurable: true });

  // Row with Linked:false so we get a "Mark linked" button (no mutation in the row).
  const p = page('current');
  p.Rows![0].Linked = false;
  p.Accounts = [{ ID: 'acc1', Label: 'acc1', Platform: 'twitch' }];

  const fetchMock = vi.fn(async (url: string) => {
    if (String(url).includes('/api/drops')) {
      return new Response(JSON.stringify(p), { status: 200, headers: { 'Content-Type': 'application/json' } });
    }
    return new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } });
  });
  vi.stubGlobal('fetch', fetchMock);

  render(Drops);
  // Wait for initial render.
  await screen.findByText('Camp current');

  const initialGetCount = fetchMock.mock.calls.filter(([u]) => String(u).includes('/api/drops?tab')).length;

  const btn = await screen.findByRole('button', { name: /mark linked/i });
  await fireEvent.click(btn);
  // Drain microtask queue so the async refetch completes.
  await new Promise((r) => setTimeout(r, 0));

  const afterGetCount = fetchMock.mock.calls.filter(([u]) => String(u).includes('/api/drops?tab')).length;
  // A second GET should have been issued (refetch after mutation).
  expect(afterGetCount).toBeGreaterThan(initialGetCount);
});
