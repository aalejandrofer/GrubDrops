import { afterEach, expect, test, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import AccountsList from './AccountsList.svelte';

afterEach(() => { vi.restoreAllMocks(); });

const rows = [{ ID: 'a1', Platform: 'twitch', DisplayName: 'acc-one', Enabled: true, AvatarURL: '', AccountInitial: 'A', State: 'watching', StateTier: 'green', StateLabel: 'state.watching', AuthChecked: true, AuthOK: true, AuthMsg: '', AuthWhen: '' }];

test('renders accounts and toggles via apiSend', async () => {
  Object.defineProperty(document, 'cookie', { configurable: true, get: () => 'csrftoken=t' });
  const fetchMock = vi.fn(async (u: string, i?: RequestInit) =>
    i?.method === 'POST'
      ? new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } })
      : new Response(JSON.stringify(rows), { status: 200, headers: { 'Content-Type': 'application/json' } }),
  );
  vi.stubGlobal('fetch', fetchMock);
  render(AccountsList);
  expect(await screen.findByText('acc-one')).toBeTruthy();
  const btn = await screen.findByRole('button', { name: /disable/i });
  await fireEvent.click(btn);
  expect(fetchMock.mock.calls.some(([u, i]) => String(u).includes('/api/accounts/a1/toggle') && (i as RequestInit)?.method === 'POST')).toBe(true);
});
