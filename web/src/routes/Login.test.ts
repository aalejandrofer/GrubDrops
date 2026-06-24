import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import Login from './Login.svelte';

// Stub csrftoken cookie (apiSend reads it)
beforeEach(() => {
  Object.defineProperty(document, 'cookie', {
    writable: true,
    value: 'csrftoken=testtoken',
  });
});

afterEach(() => {
  vi.restoreAllMocks();
});

function mockFetch(authInfo: { oidc_enabled: boolean; oidc_provider: string }, loginResponse?: { ok?: boolean; status?: number; body?: unknown }) {
  vi.stubGlobal('fetch', vi.fn(async (url: string, opts?: RequestInit) => {
    if (url === '/api/auth/info') {
      return { ok: true, status: 200, json: async () => authInfo, text: async () => JSON.stringify(authInfo) };
    }
    if (url === '/api/login') {
      const status = loginResponse?.status ?? 200;
      const body = loginResponse?.body ?? { ok: true };
      return {
        ok: status < 300,
        status,
        json: async () => body,
        text: async () => JSON.stringify(body),
        statusText: status === 400 ? 'Bad Request' : 'OK',
      };
    }
    throw new Error('Unexpected fetch: ' + url);
  }));
}

test('renders a password input and submit button', async () => {
  mockFetch({ oidc_enabled: false, oidc_provider: '' });
  render(Login);
  expect(screen.getByPlaceholderText('Password')).toBeTruthy();
  expect(screen.getByRole('button', { name: /log in/i })).toBeTruthy();
});

test('submitting calls apiSend /api/login then window.location.assign("/")', async () => {
  mockFetch({ oidc_enabled: false, oidc_provider: '' }, { status: 200, body: { ok: true } });
  const assignSpy = vi.fn();
  vi.spyOn(window, 'location', 'get').mockReturnValue({ assign: assignSpy } as unknown as Location);

  render(Login);

  const input = screen.getByPlaceholderText('Password');
  await fireEvent.input(input, { target: { value: 'hunter2' } });
  const btn = screen.getByRole('button', { name: /log in/i });
  await fireEvent.click(btn);

  await waitFor(() => {
    expect(vi.mocked(fetch)).toHaveBeenCalledWith('/api/login', expect.objectContaining({ method: 'POST' }));
    expect(assignSpy).toHaveBeenCalledWith('/');
  });
});

test('400 wrong_password shows error message', async () => {
  mockFetch(
    { oidc_enabled: false, oidc_provider: '' },
    {
      status: 400,
      body: { error: { code: 'wrong_password', message: 'Wrong password' } },
    }
  );

  render(Login);

  const input = screen.getByPlaceholderText('Password');
  await fireEvent.input(input, { target: { value: 'badpass' } });
  await fireEvent.click(screen.getByRole('button', { name: /log in/i }));

  await waitFor(() => {
    expect(screen.getByText('Wrong password')).toBeTruthy();
  });
});

test('when oidc_enabled=true renders SSO link pointing to /auth/oidc/login', async () => {
  mockFetch({ oidc_enabled: true, oidc_provider: 'Authentik' });

  render(Login);

  await waitFor(() => {
    const ssoLink = screen.getByRole('link', { name: /authentik/i });
    expect(ssoLink).toBeTruthy();
    expect(ssoLink.getAttribute('href')).toBe('/auth/oidc/login');
  });
});
