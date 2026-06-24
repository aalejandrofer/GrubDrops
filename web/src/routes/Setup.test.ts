import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import Setup from './Setup.svelte';

// Stub csrftoken cookie (apiSend reads it)
beforeEach(() => {
  Object.defineProperty(document, 'cookie', {
    writable: true,
    configurable: true,
    value: 'csrftoken=testtoken',
  });
  history.replaceState({}, '', '/setup');
});

afterEach(() => {
  vi.restoreAllMocks();
});

type AuthInfoShape = { oidc_enabled: boolean; oidc_provider: string; admin_exists: boolean };

function mockFetch(
  authInfo: AuthInfoShape,
  setupResponse?: { ok?: boolean; status?: number; body?: unknown },
) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, _opts?: RequestInit) => {
      if (url === '/api/auth/info') {
        return {
          ok: true,
          status: 200,
          json: async () => authInfo,
          text: async () => JSON.stringify(authInfo),
        };
      }
      if (url === '/api/setup') {
        const status = setupResponse?.status ?? 200;
        const body = setupResponse?.body ?? { ok: true };
        return {
          ok: status < 300,
          status,
          json: async () => body,
          text: async () => JSON.stringify(body),
          statusText: status === 400 ? 'Bad Request' : status === 409 ? 'Conflict' : 'OK',
        };
      }
      throw new Error('Unexpected fetch: ' + url);
    }),
  );
}

test('renders password and confirm inputs and a submit button', async () => {
  mockFetch({ oidc_enabled: false, oidc_provider: '', admin_exists: false });
  render(Setup);
  expect(screen.getByPlaceholderText('Password')).toBeTruthy();
  expect(screen.getByPlaceholderText('Confirm password')).toBeTruthy();
  expect(screen.getByRole('button', { name: /set password|→/i })).toBeTruthy();
});

test('successful submit calls apiSend /api/setup then window.location.assign("/")', async () => {
  mockFetch({ oidc_enabled: false, oidc_provider: '', admin_exists: false });
  const assignSpy = vi.fn();
  Object.defineProperty(window, 'location', {
    writable: true,
    configurable: true,
    value: { assign: assignSpy, href: 'http://localhost/setup' },
  });

  render(Setup);

  fireEvent.input(screen.getByPlaceholderText('Password'), { target: { value: 'goodpassword' } });
  fireEvent.input(screen.getByPlaceholderText('Confirm password'), { target: { value: 'goodpassword' } });
  fireEvent.click(screen.getByRole('button', { name: /set password|→/i }));

  await waitFor(() => {
    expect(assignSpy).toHaveBeenCalledWith('/');
  });
});

test('client-side mismatch shows error without calling /api/setup', async () => {
  const fetchSpy = vi.fn(async (url: string) => {
    if (url === '/api/auth/info') {
      return {
        ok: true,
        status: 200,
        json: async () => ({ oidc_enabled: false, oidc_provider: '', admin_exists: false }),
        text: async () => '{}',
      };
    }
    throw new Error('Should not call /api/setup on client mismatch');
  });
  vi.stubGlobal('fetch', fetchSpy);

  render(Setup);

  fireEvent.input(screen.getByPlaceholderText('Password'), { target: { value: 'password1' } });
  fireEvent.input(screen.getByPlaceholderText('Confirm password'), { target: { value: 'different1' } });
  fireEvent.click(screen.getByRole('button', { name: /set password|→/i }));

  await waitFor(() => {
    expect(screen.getByText(/passwords do not match|mismatch/i)).toBeTruthy();
  });

  // /api/setup should NOT have been called
  const setupCalls = fetchSpy.mock.calls.filter(([u]) => u === '/api/setup');
  expect(setupCalls.length).toBe(0);
});

test('client-side short password shows error without calling /api/setup', async () => {
  const fetchSpy = vi.fn(async (url: string) => {
    if (url === '/api/auth/info') {
      return {
        ok: true,
        status: 200,
        json: async () => ({ oidc_enabled: false, oidc_provider: '', admin_exists: false }),
        text: async () => '{}',
      };
    }
    throw new Error('Should not call /api/setup on short password');
  });
  vi.stubGlobal('fetch', fetchSpy);

  render(Setup);

  fireEvent.input(screen.getByPlaceholderText('Password'), { target: { value: 'short' } });
  fireEvent.input(screen.getByPlaceholderText('Confirm password'), { target: { value: 'short' } });
  fireEvent.click(screen.getByRole('button', { name: /set password|→/i }));

  await waitFor(() => {
    expect(screen.getByText(/at least 8 characters|too short/i)).toBeTruthy();
  });

  const setupCalls = fetchSpy.mock.calls.filter(([u]) => u === '/api/setup');
  expect(setupCalls.length).toBe(0);
});

test('server 400 passwords_mismatch shows error message', async () => {
  mockFetch(
    { oidc_enabled: false, oidc_provider: '', admin_exists: false },
    {
      status: 400,
      body: { error: { code: 'passwords_mismatch', message: 'Passwords do not match' } },
    },
  );

  render(Setup);

  fireEvent.input(screen.getByPlaceholderText('Password'), { target: { value: 'password1' } });
  fireEvent.input(screen.getByPlaceholderText('Confirm password'), { target: { value: 'password1' } });
  fireEvent.click(screen.getByRole('button', { name: /set password|→/i }));

  await waitFor(() => {
    expect(screen.getByText(/Passwords do not match/i)).toBeTruthy();
  });
});

test('server 409 admin_configured shows error message', async () => {
  mockFetch(
    { oidc_enabled: false, oidc_provider: '', admin_exists: false },
    {
      status: 409,
      body: { error: { code: 'admin_configured', message: 'Admin already configured' } },
    },
  );

  render(Setup);

  fireEvent.input(screen.getByPlaceholderText('Password'), { target: { value: 'password1' } });
  fireEvent.input(screen.getByPlaceholderText('Confirm password'), { target: { value: 'password1' } });
  fireEvent.click(screen.getByRole('button', { name: /set password|→/i }));

  await waitFor(() => {
    expect(screen.getByText(/Admin already configured/i)).toBeTruthy();
  });
});

test('when auth/info admin_exists:true navigates to /login', async () => {
  mockFetch({ oidc_enabled: false, oidc_provider: '', admin_exists: true });

  const pushSpy = vi.spyOn(history, 'pushState');

  render(Setup);

  await waitFor(() => {
    // navigate('/login') pushes state and changes path
    const loginCall = pushSpy.mock.calls.find(([, , url]) =>
      typeof url === 'string' && url.includes('/login'),
    );
    expect(loginCall).toBeTruthy();
  });
});
