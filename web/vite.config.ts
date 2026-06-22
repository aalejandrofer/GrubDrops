import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: '../internal/web/spa/dist',
    emptyOutDir: true,
    assetsDir: 'assets',
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/static': 'http://localhost:8080',
    },
  },
  test: {
    environment: 'jsdom',
  },
});
