import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vitest/config';
import { devProxy } from './dev-proxy.ts';

// SvelteKit 3 reads its config from the sveltekit() plugin, not svelte.config.js.
export default defineConfig({
	plugins: [
		sveltekit({
			adapter: adapter({
				pages: '../internal/web/dist',
				assets: '../internal/web/dist',
				fallback: 'index.html',
				strict: true
			})
		})
	],
	resolve: {
		alias: {
			$lib: fileURLToPath(new URL('./src/lib', import.meta.url)),
			$locales: fileURLToPath(new URL('../internal/i18n/locales', import.meta.url))
		}
	},
	server: {
		proxy: devProxy(process.env.GRUB_DEV_API ?? 'http://localhost:8080'),
		fs: { allow: ['../internal/i18n/locales', '../assets/img'] }
	},
	test: {
		environment: 'node',
		include: ['src/**/*.test.ts', 'scripts/**/*.test.ts', '*.test.ts']
	}
});
