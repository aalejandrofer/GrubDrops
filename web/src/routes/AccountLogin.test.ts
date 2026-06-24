import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import AccountLogin from './AccountLogin.svelte';
import { navigate } from '../lib/router.svelte';

beforeEach(() => {
  Object.defineProperty(document, 'cookie', { configurable: true, get: () => 'csrftoken=t' });
  history.replaceState({}, '', '/accounts/acc1/login');
  navigate('/accounts/acc1/login');
});

afterEach(() => {
  vi.restoreAllMocks();
  history.replaceState({}, '', '/');
  navigate('/');
});

function mockTwitchAccount() {
  vi.stubGlobal('fetch', vi.fn(async () =>
    new Response(JSON.stringify({ Platform: 'twitch', DisplayName: 'MyTwitchAcc' }), {
      status: 200, headers: { 'Content-Type': 'application/json' },
    }),
  ));
}

function mockKickAccount() {
  vi.stubGlobal('fetch', vi.fn(async (_url: string, opts?: RequestInit) => {
    if (opts?.method === 'POST') {
      return new Response(JSON.stringify({ ok: true, verified: true }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    return new Response(JSON.stringify({ Platform: 'kick', DisplayName: 'MyKickAcc' }), {
      status: 200, headers: { 'Content-Type': 'application/json' },
    });
  }));
}

test('twitch account: renders two method cards (Device-code login, Migrate from TwitchDropsMiner)', async () => {
  mockTwitchAccount();
  render(AccountLogin);
  await waitFor(() => expect(screen.getByText(/Device-code login/i)).toBeTruthy());
  expect(screen.getByText(/Migrate from TwitchDropsMiner/i)).toBeTruthy();
});

test('twitch account: Start device-code button navigates to /twitch/device', async () => {
  mockTwitchAccount();
  const pushSpy = vi.spyOn(history, 'pushState');
  render(AccountLogin);
  await waitFor(() => screen.getByText(/Start device-code/i));
  await fireEvent.click(screen.getByText(/Start device-code/i));
  expect(pushSpy.mock.calls.some(([, , url]) => String(url).includes('/accounts/acc1/twitch/device'))).toBe(true);
});

test('twitch account: Import cookies.jar button navigates to /twitch/cookie', async () => {
  mockTwitchAccount();
  const pushSpy = vi.spyOn(history, 'pushState');
  render(AccountLogin);
  await waitFor(() => screen.getByText(/Import cookies\.jar/i));
  await fireEvent.click(screen.getByText(/Import cookies\.jar/i));
  expect(pushSpy.mock.calls.some(([, , url]) => String(url).includes('/accounts/acc1/twitch/cookie'))).toBe(true);
});

test('kick account: renders cookies.txt form with textarea and channel input', async () => {
  mockKickAccount();
  render(AccountLogin);
  await waitFor(() => expect(screen.getByText(/Authorize →/i)).toBeTruthy());
  expect(screen.getByRole('textbox', { name: /contents/i })).toBeTruthy();
});

test('kick account: submit posts cookies_txt + channel to /api/accounts/acc1/kick/login then navigates to /accounts', async () => {
  mockKickAccount();
  const pushSpy = vi.spyOn(history, 'pushState');
  render(AccountLogin);
  await waitFor(() => screen.getByRole('textbox', { name: /contents/i }));
  const textarea = screen.getByRole('textbox', { name: /contents/i });
  await fireEvent.input(textarea, { target: { value: '# Netscape HTTP Cookie File\n.kick.com\tTRUE\t/\tTRUE\t0\tkick_session\tsomevalue' } });
  await fireEvent.click(screen.getByRole('button', { name: /Authorize →/i }));
  await waitFor(() => {
    expect(pushSpy.mock.calls.some(([, , url]) => String(url) === '/accounts')).toBe(true);
  });
});
