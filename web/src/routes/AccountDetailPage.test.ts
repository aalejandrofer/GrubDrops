import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import AccountDetailPage from './AccountDetailPage.svelte';
import { navigate } from '../lib/router.svelte';

const detail = {
  ID: 'a1', Platform: 'twitch', DisplayName: 'test-acc', Status: 'watching',
  Enabled: true, AvatarURL: '', WebhookURL: '', AllGames: null, SelectedGames: null,
  Channels: null, ForceChannels: null, ForceWatchEnabled: false,
};

const detailWithData = {
  ID: 'a1', Platform: 'twitch', DisplayName: 'test-acc', Status: 'watching',
  Enabled: true, AvatarURL: '', WebhookURL: '',
  AllGames: [
    { ID: 'g1', Name: 'Game One', Slug: 'game-one', Selected: true, Rank: 0 },
    { ID: 'g2', Name: 'Game Two', Slug: 'game-two', Selected: true, Rank: 1 },
  ],
  SelectedGames: [
    { ID: 'g1', Name: 'Game One', Slug: 'game-one', Selected: true, Rank: 0 },
    { ID: 'g2', Name: 'Game Two', Slug: 'game-two', Selected: true, Rank: 1 },
  ],
  Channels: ['chan1', 'chan2'],
  ForceChannels: ['fchan1', 'fchan2'],
  ForceWatchEnabled: false,
};

function makeFetch(initial = detail) {
  return vi.fn(async (u: string, i?: RequestInit) =>
    i?.method === 'POST'
      ? new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } })
      : new Response(JSON.stringify(initial), { status: 200, headers: { 'Content-Type': 'application/json' } }),
  );
}

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

// --- Task 2: interactive sections ---

test('games reorder (▼) posts {game_ids} to /api/accounts/a1/games', async () => {
  const fetchMock = makeFetch(detailWithData);
  vi.stubGlobal('fetch', fetchMock);
  render(AccountDetailPage);
  await screen.findByText(/Game One/);

  // click the ▼ button for the first game (moves Game One down)
  const downBtns = await screen.findAllByRole('button', { name: /▼/ });
  await fireEvent.click(downBtns[0]);

  expect(
    fetchMock.mock.calls.some(([u, i]) => {
      if (!String(u).includes('/api/accounts/a1/games')) return false;
      if ((i as RequestInit)?.method !== 'POST') return false;
      const body = JSON.parse((i as RequestInit).body as string);
      // after moving g1 down: order should be [g2, g1]
      return Array.isArray(body.game_ids) && body.game_ids[0] === 'g2' && body.game_ids[1] === 'g1';
    }),
  ).toBe(true);
});

test('add-game input posts {name} to /api/accounts/a1/games/add', async () => {
  const fetchMock = makeFetch(detailWithData);
  vi.stubGlobal('fetch', fetchMock);
  render(AccountDetailPage);
  await screen.findByText(/Game One/);

  const addInput = await screen.findByPlaceholderText(/add game/i);
  await fireEvent.input(addInput, { target: { value: 'New Game' } });
  const addBtn = addInput.closest('div')?.querySelector('button') as HTMLButtonElement;
  await fireEvent.click(addBtn);

  expect(
    fetchMock.mock.calls.some(([u, i]) => {
      if (!String(u).includes('/api/accounts/a1/games/add')) return false;
      if ((i as RequestInit)?.method !== 'POST') return false;
      const body = JSON.parse((i as RequestInit).body as string);
      return body.name === 'New Game';
    }),
  ).toBe(true);
});

test('channel add posts {channel} to /api/accounts/a1/channels/add', async () => {
  const fetchMock = makeFetch(detailWithData);
  vi.stubGlobal('fetch', fetchMock);
  render(AccountDetailPage);
  await screen.findByText('chan1');

  const addInput = await screen.findByPlaceholderText(/add channel/i);
  await fireEvent.input(addInput, { target: { value: 'newchan' } });
  const addBtn = addInput.closest('div')?.querySelector('button') as HTMLButtonElement;
  await fireEvent.click(addBtn);

  expect(
    fetchMock.mock.calls.some(([u, i]) => {
      if (!String(u).includes('/api/accounts/a1/channels/add')) return false;
      if ((i as RequestInit)?.method !== 'POST') return false;
      const body = JSON.parse((i as RequestInit).body as string);
      return body.channel === 'newchan';
    }),
  ).toBe(true);
});

test('force-watch toggle posts {enabled} to /api/accounts/a1/force-watch', async () => {
  const fetchMock = makeFetch(detailWithData);
  vi.stubGlobal('fetch', fetchMock);
  render(AccountDetailPage);
  const toggleBtn = await screen.findByRole('button', { name: /enable force watch|disable force watch/i });
  await fireEvent.click(toggleBtn);

  expect(
    fetchMock.mock.calls.some(([u, i]) => {
      if (!String(u).includes('/api/accounts/a1/force-watch')) return false;
      if ((i as RequestInit)?.method !== 'POST') return false;
      const body = JSON.parse((i as RequestInit).body as string);
      // ForceWatchEnabled was false, so toggle → enabled: true
      return body.enabled === true;
    }),
  ).toBe(true);
});
