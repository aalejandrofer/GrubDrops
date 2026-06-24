import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { isSpaPath, currentPath, navigate, startRouter } from './router.svelte';

beforeEach(() => { history.replaceState({}, '', '/'); });
afterEach(() => { vi.restoreAllMocks(); });

test('isSpaPath: / and /drops owned, /settings owned, /history not', () => {
  expect(isSpaPath('/')).toBe(true);
  expect(isSpaPath('/drops')).toBe(true);
  expect(isSpaPath('/settings')).toBe(true);
  expect(isSpaPath('/history')).toBe(false);
});

test('navigate updates currentPath and pushes history', () => {
  const push = vi.spyOn(history, 'pushState');
  navigate('/');
  expect(currentPath()).toBe('/');
  expect(push).toHaveBeenCalled();
});

test('navigate preserves query string + hash while currentPath returns only pathname', () => {
  const push = vi.spyOn(history, 'pushState');
  navigate('/?filter=x#section');
  expect(currentPath()).toBe('/');
  // pushState must be called with the full URL including query + hash
  expect(push).toHaveBeenCalledWith({}, '', '/?filter=x#section');
});

test('startRouter intercepts clicks on owned links, ignores unowned', () => {
  const teardown = startRouter();
  const pushSpy = vi.spyOn(history, 'pushState');

  // owned link → intercepted (default prevented, navigate called)
  const owned = document.createElement('a');
  owned.href = '/';
  document.body.appendChild(owned);
  const ev1 = new MouseEvent('click', { bubbles: true, cancelable: true, button: 0 });
  owned.dispatchEvent(ev1);
  expect(ev1.defaultPrevented).toBe(true);

  // unowned link → NOT intercepted (browser would navigate)
  const unowned = document.createElement('a');
  unowned.href = '/history';
  document.body.appendChild(unowned);
  const ev2 = new MouseEvent('click', { bubbles: true, cancelable: true, button: 0 });
  unowned.dispatchEvent(ev2);
  expect(ev2.defaultPrevented).toBe(false);

  teardown();
  owned.remove(); unowned.remove();
  expect(pushSpy).toBeDefined();
});

test('isSpaPath: /accounts owned, /accounts/abc owned, /accounts/new owned, /accounts/abc/login NOW owned', () => {
  expect(isSpaPath('/accounts')).toBe(true);
  expect(isSpaPath('/accounts/abc')).toBe(true);
  expect(isSpaPath('/accounts/new')).toBe(true);
  expect(isSpaPath('/accounts/abc/login')).toBe(true);
  expect(isSpaPath('/accounts/')).toBe(false);
});

test('isSpaPath: /accounts/{id}/twitch/device and /twitch/cookie are owned; /twitch/paste and /login/poll are NOT', () => {
  expect(isSpaPath('/accounts/xyz/twitch/device')).toBe(true);
  expect(isSpaPath('/accounts/xyz/twitch/cookie')).toBe(true);
  expect(isSpaPath('/accounts/xyz/twitch/paste')).toBe(false);
  expect(isSpaPath('/accounts/xyz/login/poll')).toBe(false);
});

test('isSpaPath: /login is SPA-owned', () => {
  expect(isSpaPath('/login')).toBe(true);
});

test('isSpaPath: /setup is SPA-owned', () => {
  expect(isSpaPath('/setup')).toBe(true);
});
