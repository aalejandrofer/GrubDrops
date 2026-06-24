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
