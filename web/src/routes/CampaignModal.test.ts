import { afterEach, expect, test, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import CampaignModal from './CampaignModal.svelte';
import type { CampaignDetail } from '../lib/types';

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

const detail: CampaignDetail = {
  ID: 'c1', Name: 'Camp One', Platform: 'twitch', Game: 'GameX', Status: 'active', Kind: 'drop',
  StartsAt: '2026-06-01 14:00 UTC', EndsAt: '2026-06-10 14:00 UTC', EndsIn: '5d', EndsUrgent: false,
  Benefits: [{ ID: 'b1', Name: 'Wolf Helmet', RequiredMinutes: 60, ImageURL: '' }],
  EligibleAccounts: ['acc-one'], SourceAccounts: ['acc-one'], AccountLinked: true, AccountLinkURL: '',
};

test('renders fetched campaign detail', async () => {
  vi.stubGlobal('fetch', vi.fn(async () =>
    new Response(JSON.stringify(detail), { status: 200, headers: { 'Content-Type': 'application/json' } }),
  ));
  render(CampaignModal, { props: { campaignId: 'c1', onClose: () => {} } });
  expect(await screen.findByText('Camp One')).toBeTruthy();
  expect(await screen.findByText('Wolf Helmet')).toBeTruthy();
});
