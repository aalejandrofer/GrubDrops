import type { ProxyOptions } from 'vite';

type HeaderTarget = {
	setHeader(k: string, v: string): void;
	removeHeader(k: string): void;
	getHeader(k: string): unknown;
};

// The Go side rejects cross-origin writes (nosurf). Make dev requests look
// same-origin to the miner: Origin becomes the target, Referer is dropped.
export function rewriteOrigin(headers: HeaderTarget, target: string): void {
	if (headers.getHeader('origin') !== undefined) {
		headers.setHeader('origin', new URL(target).origin);
	}
	headers.removeHeader('referer');
}

export function devProxy(target: string): Record<string, ProxyOptions> {
	const entry = (): ProxyOptions => ({
		target,
		changeOrigin: true,
		configure: (proxy) => {
			proxy.on('proxyReq', (proxyReq) => rewriteOrigin(proxyReq, target));
		}
	});
	return { '/api': entry(), '/auth': entry() };
}
