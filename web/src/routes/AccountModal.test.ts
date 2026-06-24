import { afterEach, expect, test, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import AccountModal from './AccountModal.svelte';
import type { AccountDetail } from '../lib/types';

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

const detail: AccountDetail = {
  ID: 'a1', Platform: 'twitch', DisplayName: 'acc-one', Enabled: true,
  State: 'watching', StateLabel: 'Watching', CurrentCampaign: 'Camp', CurrentGame: 'Game',
  CurrentChannel: 'chan', ProgressPct: 42, WatchETA: '01:00', Uptime: '5m',
  Games: [{ Rank: 1, Name: 'Game' }], EligibleCampaigns: [], UpcomingCampaigns: [],
};

test('renders fetched detail and toggles via apiSend', async () => {
  Object.defineProperty(document, 'cookie', { value: 'csrftoken=t', writable: true, configurable: true });
  const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === 'POST') return new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    return new Response(JSON.stringify(detail), { status: 200, headers: { 'Content-Type': 'application/json' } });
  });
  vi.stubGlobal('fetch', fetchMock);

  render(AccountModal, { props: { accountId: 'a1', onClose: () => {} } });
  expect(await screen.findByText('acc-one')).toBeTruthy();

  const btn = await screen.findByRole('button', { name: /disable/i });
  await fireEvent.click(btn);
  // a POST to the toggle endpoint happened
  expect(fetchMock.mock.calls.some(([u, i]) => String(u).includes('/api/accounts/a1/toggle') && (i as RequestInit)?.method === 'POST')).toBe(true);
});

test('shows loading state before fetch resolves', async () => {
  let resolveDetail!: (d: AccountDetail) => void;
  const fetchMock = vi.fn(async () => {
    await new Promise<void>((res) => { resolveDetail = () => res(); });
    return new Response(JSON.stringify(detail), { status: 200, headers: { 'Content-Type': 'application/json' } });
  });
  vi.stubGlobal('fetch', fetchMock);
  render(AccountModal, { props: { accountId: 'a1', onClose: () => {} } });
  expect(screen.getByText(/loading/i)).toBeTruthy();
  resolveDetail(detail);
  expect(await screen.findByText('acc-one')).toBeTruthy();
});

test('calls onClose when close button is clicked', async () => {
  vi.stubGlobal('fetch', vi.fn(async () =>
    new Response(JSON.stringify(detail), { status: 200, headers: { 'Content-Type': 'application/json' } })
  ));
  const onClose = vi.fn();
  render(AccountModal, { props: { accountId: 'a1', onClose } });
  await screen.findByText('acc-one');
  const closeBtn = screen.getByRole('button', { name: /close/i });
  await fireEvent.click(closeBtn);
  expect(onClose).toHaveBeenCalledOnce();
});

test('calls onClose on Escape key', async () => {
  vi.stubGlobal('fetch', vi.fn(async () =>
    new Response(JSON.stringify(detail), { status: 200, headers: { 'Content-Type': 'application/json' } })
  ));
  const onClose = vi.fn();
  render(AccountModal, { props: { accountId: 'a1', onClose } });
  await screen.findByText('acc-one');
  await fireEvent.keyDown(document, { key: 'Escape' });
  expect(onClose).toHaveBeenCalledOnce();
});

test('issues Reload POST', async () => {
  Object.defineProperty(document, 'cookie', { value: 'csrftoken=t', writable: true, configurable: true });
  const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
    if (init?.method === 'POST') return new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    return new Response(JSON.stringify(detail), { status: 200, headers: { 'Content-Type': 'application/json' } });
  });
  vi.stubGlobal('fetch', fetchMock);
  render(AccountModal, { props: { accountId: 'a1', onClose: () => {} } });
  await screen.findByText('acc-one');
  await fireEvent.click(screen.getByRole('button', { name: /reload/i }));
  expect(fetchMock.mock.calls.some(([u, i]) => String(u).includes('/api/accounts/a1/reload') && (i as RequestInit)?.method === 'POST')).toBe(true);
});

test('issues Force-watch POST', async () => {
  Object.defineProperty(document, 'cookie', { value: 'csrftoken=t', writable: true, configurable: true });
  const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
    if (init?.method === 'POST') return new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    return new Response(JSON.stringify(detail), { status: 200, headers: { 'Content-Type': 'application/json' } });
  });
  vi.stubGlobal('fetch', fetchMock);
  render(AccountModal, { props: { accountId: 'a1', onClose: () => {} } });
  await screen.findByText('acc-one');
  await fireEvent.click(screen.getByRole('button', { name: /force.watch/i }));
  expect(fetchMock.mock.calls.some(([u, i]) => String(u).includes('/api/accounts/a1/force-watch') && (i as RequestInit)?.method === 'POST')).toBe(true);
});
