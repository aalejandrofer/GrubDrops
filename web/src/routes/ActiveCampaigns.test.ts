import { expect, test } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { vi } from 'vitest';
import ActiveCampaigns from './ActiveCampaigns.svelte';

const camp = { ID: 'c1', Name: 'Camp One', Platform: 'twitch', Game: 'GameX', Kind: 'drop', Drops: 3, Channels: 5, EndsIn: '12h', EndsUrgent: true, Claimed: 1, Total: 3 };

test('renders a campaign row with claimed/total and ends-in', () => {
  render(ActiveCampaigns, { props: { camps: [camp] } });
  expect(screen.getByText('Camp One')).toBeTruthy();
  expect(screen.getByText('1 / 3')).toBeTruthy();
  expect(screen.getByText('12h')).toBeTruthy();
});

test('marks urgent campaigns', () => {
  const { container } = render(ActiveCampaigns, { props: { camps: [camp] } });
  expect(container.querySelector('.urgent')).not.toBeNull();
});

test('renders nothing when empty', () => {
  const { container } = render(ActiveCampaigns, { props: { camps: null } });
  expect(container.querySelector('.campaign-row')).toBeNull();
});

test('clicking a campaign row fires onSelect with the id', async () => {
  const onSelect = vi.fn();
  render(ActiveCampaigns, { props: { camps: [camp], onSelect } });
  await fireEvent.click(screen.getByText('Camp One'));
  expect(onSelect).toHaveBeenCalledWith('c1');
});
