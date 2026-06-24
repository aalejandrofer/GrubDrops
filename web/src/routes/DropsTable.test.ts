import { expect, test } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import DropsTable from './DropsTable.svelte';
import type { DropRow } from '../lib/types';

const row: DropRow = {
  CampaignID: 'c1', When: '2026-06-10', Platform: 'twitch', Game: 'GameX',
  CampaignName: 'Camp One', BenefitName: 'Wolf Helmet', AccountName: 'acc', Kind: 'drop',
  ActionOnly: false, Collectors: [{ Login: 'acc', Platform: 'twitch', Full: true }],
  Channels: null, WhitelistedBy: null, Linked: true, LinkURL: '',
  ConnectChips: null, NeedsConnect: false,
};

test('renders a drop row', () => {
  render(DropsTable, { props: { rows: [row] } });
  expect(screen.getByText('Camp One')).toBeTruthy();
  expect(screen.getByText('Wolf Helmet')).toBeTruthy();
  expect(screen.getByText('GameX')).toBeTruthy();
});

test('renders nothing for empty rows', () => {
  const { container } = render(DropsTable, { props: { rows: null } });
  expect(container.querySelector('.drop-row')).toBeNull();
});
