import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/svelte';
import AccountTwitchDevice from './AccountTwitchDevice.svelte';
import { navigate } from '../lib/router.svelte';

beforeEach(() => {
  Object.defineProperty(document, 'cookie', { configurable: true, get: () => 'csrftoken=t' });
  history.replaceState({}, '', '/accounts/acc1/twitch/device');
  navigate('/accounts/acc1/twitch/device');
  vi.useFakeTimers();
});

afterEach(() => {
  vi.runOnlyPendingTimers();
  vi.useRealTimers();
  vi.restoreAllMocks();
  history.replaceState({}, '', '/');
  navigate('/');
});

test('start: POST to /twitch/device shows user_code and verification_url', async () => {
  vi.stubGlobal('fetch', vi.fn(async (url: string, opts?: RequestInit) => {
    if (String(url).includes('/twitch/device') && opts?.method === 'POST') {
      return new Response(JSON.stringify({ user_code: 'ABCD12', verification_url: 'https://activate.twitch.tv' }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    // account detail fetch
    return new Response(JSON.stringify({ Platform: 'twitch', DisplayName: 'Acc1' }), {
      status: 200, headers: { 'Content-Type': 'application/json' },
    });
  }));

  render(AccountTwitchDevice);

  await waitFor(() => expect(screen.getByText('ABCD12')).toBeTruthy());
  expect(screen.getByText(/activate\.twitch\.tv/)).toBeTruthy();
});

test('poll done: navigate to /accounts', async () => {
  let pollCount = 0;
  vi.stubGlobal('fetch', vi.fn(async (url: string, opts?: RequestInit) => {
    if (String(url).includes('/twitch/device') && opts?.method === 'POST') {
      return new Response(JSON.stringify({ user_code: 'XYZW99', verification_url: 'https://activate.twitch.tv' }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    if (String(url).includes('/twitch/poll')) {
      pollCount++;
      return new Response(JSON.stringify({ status: 'done' }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    return new Response(JSON.stringify({ Platform: 'twitch', DisplayName: 'Acc1' }), {
      status: 200, headers: { 'Content-Type': 'application/json' },
    });
  }));

  const pushSpy = vi.spyOn(history, 'pushState');
  render(AccountTwitchDevice);

  // Wait for start to complete and code to appear
  await waitFor(() => expect(screen.getByText('XYZW99')).toBeTruthy());

  // Advance timer to trigger poll
  await vi.advanceTimersByTimeAsync(3100);

  await waitFor(() => {
    expect(pollCount).toBeGreaterThan(0);
    expect(pushSpy.mock.calls.some(([, , url]) => String(url) === '/accounts')).toBe(true);
  });
});

test('poll expired: shows expired message and retry button', async () => {
  vi.stubGlobal('fetch', vi.fn(async (url: string, opts?: RequestInit) => {
    if (String(url).includes('/twitch/device') && opts?.method === 'POST') {
      return new Response(JSON.stringify({ user_code: 'EXPD00', verification_url: 'https://activate.twitch.tv' }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    if (String(url).includes('/twitch/poll')) {
      return new Response(JSON.stringify({ status: 'expired' }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    return new Response(JSON.stringify({ Platform: 'twitch', DisplayName: 'Acc1' }), {
      status: 200, headers: { 'Content-Type': 'application/json' },
    });
  }));

  render(AccountTwitchDevice);
  await waitFor(() => screen.getByText('EXPD00'));
  await vi.advanceTimersByTimeAsync(3100);
  await waitFor(() => expect(screen.getByText(/Code expired/i)).toBeTruthy());
  expect(screen.getByRole('button', { name: /Retry/i })).toBeTruthy();
});
