import { defineConfig } from 'vitest/config'
import { svelte } from '@sveltejs/vite-plugin-svelte'

export default defineConfig({
  // Vitest uses this file instead of vite.config.ts and does NOT merge it —
  // keep plugins/resolve in sync with vite.config.ts by hand.
  plugins: [svelte()],
  test: {
    environment: 'node',
    setupFiles: ['./vitest.setup.ts'],
  }
})
