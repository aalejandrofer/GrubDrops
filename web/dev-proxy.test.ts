import { expect, test } from 'vitest';
import { devProxy, rewriteOrigin } from './dev-proxy';

test('rewriteOrigin sets target origin and drops referer', () => {
	const h: Record<string, string> = {
		origin: 'http://localhost:5173',
		referer: 'http://localhost:5173/games'
	};
	rewriteOrigin(
		{
			setHeader: (k, v) => {
				h[k] = v;
			},
			removeHeader: (k) => {
				delete h[k];
			},
			getHeader: (k) => h[k]
		},
		'http://127.0.0.1:18080'
	);
	expect(h.origin).toBe('http://127.0.0.1:18080');
	expect(h.referer).toBeUndefined();
});

test('devProxy covers /api and /auth', () => {
	const p = devProxy('http://127.0.0.1:18080');
	expect(Object.keys(p)).toEqual(['/api', '/auth']);
	expect(p['/api'].target).toBe('http://127.0.0.1:18080');
	expect(p['/auth'].target).toBe('http://127.0.0.1:18080');
});
