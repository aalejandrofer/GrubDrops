import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import AccountDetailPage from './AccountDetailPage.svelte';
import { navigate } from '../lib/router.svelte';

const detail = {
  ID: 'a1', Platform: 'twitch', DisplayName: 'test-acc', Status: 'watching',
  Enabled: true, AvatarURL: '', WebhookURL: '', AllGames: null, SelectedGames: null,
  Channels: null, ForceChannels: null, ForceWatchEnabled: false,
};

beforeEach(() => {
  Object.defineProperty(document, 'cookie', { configurable: true, get: () => 'csrftoken=t' });
  history.replaceState({}, '', '/accounts/a1');
  navigate('/accounts/a1');
});

afterEach(() => {
  vi.restoreAllMocks();
  history.replaceState({}, '', '/');
  navigate('/');
});

test('renders fetched account detail', async () => {
  const fetchMock = vi.fn(async () =>
    new Response(JSON.stringify(detail), { status: 200, headers: { 'Content-Type': 'application/json' } }),
  );
  vi.stubGlobal('fetch', fetchMock);
  render(AccountDetailPage);
  expect(await screen.findByText('test-acc')).toBeTruthy();
  expect(fetchMock.mock.calls.some(([u]) => String(u).includes('/api/accounts/a1'))).toBe(true);
});

test('Save button posts to /api/accounts/a1/update', async () => {
  const fetchMock = vi.fn(async (u: string, i?: RequestInit) =>
    i?.method === 'POST'
      ? new Response(JSON.stringify({}), { status: 200, headers: { 'Content-Type': 'application/json' } })
      : new Response(JSON.stringify(detail), { status: 200, headers: { 'Content-Type': 'application/json' } }),
  );
  vi.stubGlobal('fetch', fetchMock);
  render(AccountDetailPage);
  await screen.findByText('test-acc');
  const saveBtn = await screen.findByRole('button', { name: /save/i });
  await fireEvent.click(saveBtn);
  expect(
    fetchMock.mock.calls.some(([u, i]) =>
      String(u).includes('/api/accounts/a1/update') && (i as RequestInit)?.method === 'POST',
    ),
  ).toBe(true);
});
