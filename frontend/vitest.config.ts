import { defineConfig } from 'vitest/config'
import { svelte } from '@sveltejs/vite-plugin-svelte'

export default defineConfig({
  // Vitest uses this file instead of vite.config.ts and does NOT merge it -
  // keep plugins/resolve in sync with vite.config.ts by hand. The svelte plugin
  // reads svelte.config.js (which sets vitePreprocess({ style: false }) so tests
  // compile without vite 6's preprocessCSS environment).
  plugins: [svelte()],
  test: {
    name: 'ssr',
    environment: 'node',
    setupFiles: ['./vitest.setup.ts'],
    // DOM interaction tests run under vitest.dom.config.ts (happy-dom + the
    // browser build); they must not also be picked up here, where the SSR build
    // has no mount().
    exclude: ['**/node_modules/**', '**/*.conflict.test.ts', '**/*.dom.test.ts'],
  }
})
