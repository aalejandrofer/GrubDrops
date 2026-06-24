import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import AccountNew from './AccountNew.svelte';

beforeEach(() => {
  Object.defineProperty(document, 'cookie', { configurable: true, get: () => 'csrftoken=t' });
  history.replaceState({}, '', '/accounts/new');
});

afterEach(() => {
  vi.restoreAllMocks();
  history.replaceState({}, '', '/');
});

test('renders platform select and display name input', () => {
  vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } })));
  render(AccountNew);
  expect(screen.getByRole('combobox')).toBeTruthy();
  expect(screen.getByPlaceholderText(/account label/i)).toBeTruthy();
});

test('Create posts {platform, display_name} to /api/accounts/new and navigates to /accounts/<id>', async () => {
  const fetchMock = vi.fn(async () =>
    new Response(JSON.stringify({ ok: true, id: 'newid' }), { status: 200, headers: { 'Content-Type': 'application/json' } }),
  );
  vi.stubGlobal('fetch', fetchMock);
  const pushSpy = vi.spyOn(history, 'pushState');

  render(AccountNew);

  const nameInput = screen.getByPlaceholderText(/account label/i);
  await fireEvent.input(nameInput, { target: { value: 'MyAccount' } });

  const createBtn = screen.getByRole('button', { name: /create/i });
  await fireEvent.click(createBtn);

  // wait for async
  await vi.waitFor(() => {
    expect(fetchMock.mock.calls.some(([u, i]) => {
      if (!String(u).includes('/api/accounts/new')) return false;
      if ((i as RequestInit)?.method !== 'POST') return false;
      const body = JSON.parse((i as RequestInit).body as string);
      return body.platform === 'twitch' && body.display_name === 'MyAccount';
    })).toBe(true);
  });

  expect(pushSpy.mock.calls.some(([, , url]) => String(url).includes('/accounts/newid'))).toBe(true);
});
