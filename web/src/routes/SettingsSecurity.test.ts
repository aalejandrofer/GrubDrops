import { afterEach, expect, test, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import SettingsSecurity from './SettingsSecurity.svelte';

afterEach(() => { vi.restoreAllMocks(); });

const oidcPayload = {
  OIDC: {
    Enabled: true,
    ProviderName: 'TestProvider',
    Issuer: 'https://issuer.example.com',
    CallbackURL: 'https://app.example.com/auth/callback',
    AllowedEmails: ['admin@example.com'],
    AllowedGroups: ['grubdrops-users'],
  },
};

test('renders OIDC info and password form', async () => {
  Object.defineProperty(document, 'cookie', { configurable: true, get: () => 'csrftoken=t' });
  vi.stubGlobal('fetch', vi.fn(async () =>
    new Response(JSON.stringify(oidcPayload), { status: 200, headers: { 'Content-Type': 'application/json' } }),
  ));
  render(SettingsSecurity);
  // password form fields
  expect(await screen.findByLabelText(/current password/i)).toBeTruthy();
  expect(screen.getByLabelText('New Password')).toBeTruthy();
  expect(screen.getByLabelText(/confirm new password/i)).toBeTruthy();
  // OIDC block
  expect(await screen.findByText(/TestProvider/i)).toBeTruthy();
  expect(screen.getByText(/https:\/\/issuer\.example\.com/i)).toBeTruthy();
});

test('submitting password form posts to /api/settings/password', async () => {
  Object.defineProperty(document, 'cookie', { configurable: true, get: () => 'csrftoken=t' });
  const fetchMock = vi.fn(async (u: string, i?: RequestInit) =>
    (i?.method === 'POST')
      ? new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } })
      : new Response(JSON.stringify(oidcPayload), { status: 200, headers: { 'Content-Type': 'application/json' } }),
  );
  vi.stubGlobal('fetch', fetchMock);
  render(SettingsSecurity);
  const currentInput = await screen.findByLabelText(/current password/i);
  const newInput = screen.getByLabelText('New Password');
  const confirmInput = screen.getByLabelText(/confirm new password/i);
  await fireEvent.input(currentInput, { target: { value: 'oldpass' } });
  await fireEvent.input(newInput, { target: { value: 'newpass' } });
  await fireEvent.input(confirmInput, { target: { value: 'newpass' } });
  const btn = screen.getByRole('button', { name: /change password/i });
  await fireEvent.click(btn);
  expect(fetchMock.mock.calls.some(([u, i]) =>
    String(u).includes('/api/settings/password') && (i as RequestInit)?.method === 'POST',
  )).toBe(true);
});

test('shows error message on 400 response', async () => {
  Object.defineProperty(document, 'cookie', { configurable: true, get: () => 'csrftoken=t' });
  const errorBody = { error: { code: 'current_password_wrong', message: 'Current password is incorrect' } };
  const fetchMock = vi.fn(async (u: string, i?: RequestInit) =>
    (i?.method === 'POST')
      ? new Response(JSON.stringify(errorBody), { status: 400, headers: { 'Content-Type': 'application/json' } })
      : new Response(JSON.stringify(oidcPayload), { status: 200, headers: { 'Content-Type': 'application/json' } }),
  );
  vi.stubGlobal('fetch', fetchMock);
  render(SettingsSecurity);
  const currentInput = await screen.findByLabelText(/current password/i);
  await fireEvent.input(currentInput, { target: { value: 'wrongpass' } });
  const btn = screen.getByRole('button', { name: /change password/i });
  await fireEvent.click(btn);
  expect(await screen.findByText(/Current password is incorrect/i)).toBeTruthy();
});
