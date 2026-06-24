import { afterEach, expect, test, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import Priority from './Priority.svelte';

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

const view = {
  GlobalGames: [{ ID: 'g1', Name: 'Game One', Selected: true }, { ID: 'g2', Name: 'Game Two', Selected: true }],
  AllGames: [{ ID: 'g1', Name: 'Game One', Selected: true }, { ID: 'g2', Name: 'Game Two', Selected: true }, { ID: 'g3', Name: 'Game Three', Selected: false }],
  PriorityMode: 'ordered',
};

test('renders ordered games and reorders via apiSend', async () => {
  Object.defineProperty(document, 'cookie', { value: 'csrftoken=t', configurable: true });
  const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === 'POST') return new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    return new Response(JSON.stringify(view), { status: 200, headers: { 'Content-Type': 'application/json' } });
  });
  vi.stubGlobal('fetch', fetchMock);

  render(Priority);
  expect(await screen.findByText('Game One')).toBeTruthy();
  // move Game One down
  const downBtns = await screen.findAllByRole('button', { name: /down|▼/i });
  await fireEvent.click(downBtns[0]);
  expect(fetchMock.mock.calls.some(([u, i]) => String(u).includes('/api/settings/global-games') && (i as RequestInit)?.method === 'POST')).toBe(true);
});
