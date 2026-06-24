import { expect, test } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import EventsDrawer from './EventsDrawer.svelte';
import type { DashEvent } from '../lib/types';

const events: DashEvent[] = [
  { ID: 'e1', Time: '14:01', Kind: 'claim', Color: 'green', BodyHTML: '<b>Claimed Helmet</b>', Account: 'acc1', Platform: 'twitch', Details: null },
  { ID: 'e2', Time: '14:02', Kind: 'error', Color: 'red', BodyHTML: '<b>Boom</b>', Account: 'acc2', Platform: 'kick', Details: null },
];

test('renders all events and their HTML bodies', () => {
  render(EventsDrawer, { props: { events, accounts: null } });
  expect(screen.getByText('Claimed Helmet')).toBeTruthy();
  expect(screen.getByText('Boom')).toBeTruthy();
});

test('filters by kind', async () => {
  render(EventsDrawer, { props: { events, accounts: null } });
  await fireEvent.click(screen.getByRole('button', { name: /error/i }));
  expect(screen.queryByText('Claimed Helmet')).toBeNull();
  expect(screen.getByText('Boom')).toBeTruthy();
});
