import { afterEach, expect, test, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import SettingsNotifications from './SettingsNotifications.svelte';

afterEach(() => { vi.restoreAllMocks(); });

const view = {
  GlobalGames: null, AllGames: null, PriorityMode: 'ordered',
  LogRetentionDays: 14, LogLevel: 'info', LogLevelEnv: 'info',
  TickIntervalSec: 60, DiscoveryIntervalMin: 10,
  Version: 'v1.3.0', GitCommit: 'abc', GoVersion: 'go1.26', Uptime: '1h', Goroutines: 12,
  BrowserURL: '', Sidecars: null,
  GlobalDiscordWebhook: 'https://discord.example/hook', NotifyAvatarURL: '',
  NotifyClaim: true, NotifyProgress: false, NotifyAuth: true, NotifyError: true, NotifyCanary: false,
  ProgressNotifyStep: 25,
  KickWatchMode: 'auto', ProxyURL: '', ProxyEnabled: false,
};

test('renders notification values and saves', async () => {
  Object.defineProperty(document, 'cookie', { configurable: true, get: () => 'csrftoken=t' });
  const fetchMock = vi.fn(async (u: string, i?: RequestInit) =>
    i?.method === 'POST'
      ? new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } })
      : new Response(JSON.stringify(view), { status: 200, headers: { 'Content-Type': 'application/json' } }),
  );
  vi.stubGlobal('fetch', fetchMock);
  render(SettingsNotifications);
  expect(await screen.findByDisplayValue('https://discord.example/hook')).toBeTruthy();
  const save = await screen.findByRole('button', { name: /save/i });
  await fireEvent.click(save);
  expect(fetchMock.mock.calls.some(([u, i]) => String(u).includes('/api/settings/notifications') && (i as RequestInit)?.method === 'POST')).toBe(true);
});
