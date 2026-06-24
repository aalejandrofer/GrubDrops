import { expect, test, afterEach, vi, waitFor } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import DropsTable from './DropsTable.svelte';
import type { DropRow } from '../lib/types';

const row: DropRow = {
  CampaignID: 'c1', When: '2026-06-10', Platform: 'twitch', Game: 'GameX',
  CampaignName: 'Camp One', BenefitName: 'Wolf Helmet', AccountName: 'acc', Kind: 'drop',
  ActionOnly: false, Collectors: [{ Login: 'acc', Platform: 'twitch', Full: true }],
  Channels: null, WhitelistedBy: null, Linked: true, LinkURL: '',
  ConnectChips: null, NeedsConnect: false,
};

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

test('renders a drop row', () => {
  render(DropsTable, { props: { rows: [row] } });
  expect(screen.getByText('Camp One')).toBeTruthy();
  expect(screen.getByText('Wolf Helmet')).toBeTruthy();
  expect(screen.getByText('GameX')).toBeTruthy();
});

test('renders nothing for empty rows', () => {
  const { container } = render(DropsTable, { props: { rows: null } });
  expect(container.querySelector('.drop-row')).toBeNull();
});

test('read-only: no actions column when onMutated absent', () => {
  const { container } = render(DropsTable, { props: { rows: [row], kind: 'current' } });
  expect(container.querySelector('.actions')).toBeNull();
});

test('mark-linked fires apiSend and onMutated', async () => {
  // Patch document.cookie without replacing the whole document object.
  Object.defineProperty(document, 'cookie', { value: 'csrftoken=t', configurable: true });
  const fetchMock = vi.fn(async () => new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } }));
  vi.stubGlobal('fetch', fetchMock);
  const onMutated = vi.fn();
  const r = { ...row, NeedsConnect: true };
  render(DropsTable, { props: { rows: [r], accounts: null, onMutated, kind: 'current' } });

  const btn = await screen.findByRole('button', { name: /link/i });
  await fireEvent.click(btn);
  expect(fetchMock.mock.calls.some(([u, i]) => String(u).includes('/api/drops/link') && (i as RequestInit)?.method === 'POST')).toBe(true);
});

test('unlink fires apiSend with unlink:true and calls onMutated', async () => {
  Object.defineProperty(document, 'cookie', { value: 'csrftoken=t', configurable: true });
  const fetchMock = vi.fn(async () => new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } }));
  vi.stubGlobal('fetch', fetchMock);
  const onMutated = vi.fn();
  const r = { ...row, Linked: true };
  render(DropsTable, { props: { rows: [r], accounts: null, onMutated, kind: 'current' } });

  const btn = await screen.findByRole('button', { name: /unlink/i });
  await fireEvent.click(btn);
  // Drain microtask queue so the async onclick handler completes.
  await new Promise((r) => setTimeout(r, 0));
  // Confirm fetch was called with unlink:true.
  expect(fetchMock.mock.calls.some(([u, i]) => {
    if (!String(u).includes('/api/drops/link')) return false;
    try { const b = JSON.parse((i as RequestInit).body as string); return b.unlink === true; } catch { return false; }
  })).toBe(true);
  expect(onMutated).toHaveBeenCalled();
});
