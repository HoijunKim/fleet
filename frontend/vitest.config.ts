import { defineConfig } from 'vitest/config'
import { svelte } from '@sveltejs/vite-plugin-svelte'

export default defineConfig({
  // Vitest uses this file instead of vite.config.ts and does NOT merge it —
  // keep plugins/resolve in sync with vite.config.ts by hand. The svelte plugin
  // reads svelte.config.js (which sets vitePreprocess({ style: false }) so tests
  // compile without vite 6's preprocessCSS environment).
  plugins: [svelte()],
  test: {
    environment: 'node',
    setupFiles: ['./vitest.setup.ts'],
  }
})
