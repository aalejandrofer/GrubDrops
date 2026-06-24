import { afterEach, expect, test, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import SettingsGeneral from './SettingsGeneral.svelte';

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

const view = {
  GlobalGames: null, AllGames: null, PriorityMode: 'ordered',
  LogRetentionDays: 14, LogLevel: 'info', LogLevelEnv: 'info',
  TickIntervalSec: 60, DiscoveryIntervalMin: 10,
  Version: 'v1.3.0', GitCommit: 'abc', GoVersion: 'go1.26', Uptime: '1h', Goroutines: 12,
  BrowserURL: '', Sidecars: null,
};

test('renders values and saves via apiSend', async () => {
  // Patch document.cookie without replacing the whole document object.
  Object.defineProperty(document, 'cookie', { value: 'csrftoken=t', writable: true, configurable: true });
  const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === 'POST') return new Response(JSON.stringify({ ok: true, intervals_changed: true }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    return new Response(JSON.stringify(view), { status: 200, headers: { 'Content-Type': 'application/json' } });
  });
  vi.stubGlobal('fetch', fetchMock);

  render(SettingsGeneral);
  // wait for the version/diagnostic to render
  expect(await screen.findByText('v1.3.0')).toBeTruthy();
  const save = await screen.findByRole('button', { name: /save/i });
  await fireEvent.click(save);
  expect(fetchMock.mock.calls.some(([u, i]) => String(u).includes('/api/settings/general') && (i as RequestInit)?.method === 'POST')).toBe(true);
});
