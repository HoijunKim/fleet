import { defineConfig } from 'vitest/config'
import { svelte } from '@sveltejs/vite-plugin-svelte'

// A second vitest project for DOM component tests (*.dom-style click-wiring),
// separate from the default SSR config so the two Svelte compilation modes do
// not collide. Runs only *.conflict/*.dom test files under happy-dom with the
// browser client build.
export default defineConfig({
  plugins: [svelte({ compilerOptions: { hmr: false } })],
  resolve: { conditions: ['browser'] },
  test: {
    name: 'dom',
    environment: 'happy-dom',
    setupFiles: ['./vitest.setup.ts'],
    include: ['src/**/*.conflict.test.ts', 'src/**/*.dom.test.ts'],
    server: { deps: { inline: [/svelte/, /@testing-library/] } },
  },
})
