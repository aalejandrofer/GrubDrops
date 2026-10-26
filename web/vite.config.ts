import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
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
			}),
			alias: { $locales: '../internal/i18n/locales' }
		})
	],
	server: {
		proxy: devProxy(process.env.GRUB_DEV_API ?? 'http://localhost:8080'),
		fs: { allow: ['../internal/i18n/locales', '../assets/img'] }
	},
	test: {
		environment: 'node',
		include: ['src/**/*.test.ts', 'scripts/**/*.test.ts', '*.test.ts']
	}
});
