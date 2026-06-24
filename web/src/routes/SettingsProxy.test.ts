import { afterEach, expect, test, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import SettingsProxy from './SettingsProxy.svelte';

afterEach(() => { vi.restoreAllMocks(); });

test('renders proxy values and saves', async () => {
  Object.defineProperty(document, 'cookie', { configurable: true, get: () => 'csrftoken=t' });
  const fetchMock = vi.fn(async (u: string, i?: RequestInit) =>
    i?.method === 'POST'
      ? new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } })
      : new Response(JSON.stringify({ ProxyURL: 'socks5://x', ProxyEnabled: true }), { status: 200, headers: { 'Content-Type': 'application/json' } }),
  );
  vi.stubGlobal('fetch', fetchMock);
  render(SettingsProxy);
  const save = await screen.findByRole('button', { name: /save/i });
  await fireEvent.click(save);
  expect(fetchMock.mock.calls.some(([u, i]) => String(u).includes('/api/settings/proxy') && (i as RequestInit)?.method === 'POST')).toBe(true);
});
