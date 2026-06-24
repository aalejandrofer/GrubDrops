import { afterEach, expect, test, vi } from 'vitest';
import { readCookie } from './csrf';

afterEach(() => vi.unstubAllGlobals());

test('reads a named cookie', () => {
  vi.stubGlobal('document', { cookie: 'a=1; csrftoken=abc123; b=2' } as Document);
  expect(readCookie('csrftoken')).toBe('abc123');
});

test('returns null when absent', () => {
  vi.stubGlobal('document', { cookie: 'a=1' } as Document);
  expect(readCookie('csrftoken')).toBeNull();
});
