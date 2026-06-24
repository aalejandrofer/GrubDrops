import { afterEach, expect, test, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import SettingsHealth from './SettingsHealth.svelte';

afterEach(() => { vi.restoreAllMocks(); });

const healthPayload = {
  CanaryTwitchChannel: 'alveussanctuary',
  CanaryKickChannel: 'kick',
  CanaryIntervalSec: 21600,
  CanaryTwitch: { Configured: true, OK: true, Detail: 'WS transport OK', When: '2026-06-24T00:00:00Z' },
  CanaryKick: { Configured: true, OK: false, Detail: 'WS timeout', When: '2026-06-24T01:00:00Z' },
};

test('renders canary results and form fields', async () => {
  Object.defineProperty(document, 'cookie', { configurable: true, get: () => 'csrftoken=t' });
  vi.stubGlobal('fetch', vi.fn(async () =>
    new Response(JSON.stringify(healthPayload), { status: 200, headers: { 'Content-Type': 'application/json' } }),
  ));
  render(SettingsHealth);
  // canary channel fields
  expect(await screen.findByDisplayValue('alveussanctuary')).toBeTruthy();
  expect(screen.getByDisplayValue('kick')).toBeTruthy();
  // canary result detail
  expect(await screen.findByText(/WS transport OK/i)).toBeTruthy();
  expect(screen.getByText(/WS timeout/i)).toBeTruthy();
});

test('Save button posts to /api/settings/canary', async () => {
  Object.defineProperty(document, 'cookie', { configurable: true, get: () => 'csrftoken=t' });
  const fetchMock = vi.fn(async (u: string, i?: RequestInit) =>
    (i?.method === 'POST')
      ? new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } })
      : new Response(JSON.stringify(healthPayload), { status: 200, headers: { 'Content-Type': 'application/json' } }),
  );
  vi.stubGlobal('fetch', fetchMock);
  render(SettingsHealth);
  const btn = await screen.findByRole('button', { name: /^save$/i });
  await fireEvent.click(btn);
  expect(fetchMock.mock.calls.some(([u, i]) =>
    String(u).includes('/api/settings/canary') &&
    !String(u).includes('/run') &&
    (i as RequestInit)?.method === 'POST',
  )).toBe(true);
});

test('Run now button posts to /api/settings/canary/run', async () => {
  Object.defineProperty(document, 'cookie', { configurable: true, get: () => 'csrftoken=t' });
  const fetchMock = vi.fn(async (u: string, i?: RequestInit) =>
    (i?.method === 'POST')
      ? new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } })
      : new Response(JSON.stringify(healthPayload), { status: 200, headers: { 'Content-Type': 'application/json' } }),
  );
  vi.stubGlobal('fetch', fetchMock);
  render(SettingsHealth);
  const btn = await screen.findByRole('button', { name: /run now/i });
  await fireEvent.click(btn);
  expect(fetchMock.mock.calls.some(([u, i]) =>
    String(u).includes('/api/settings/canary/run') && (i as RequestInit)?.method === 'POST',
  )).toBe(true);
});
