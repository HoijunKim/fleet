import { vitePreprocess } from '@sveltejs/vite-plugin-svelte'

export default {
  // Svelte 5 uses vite-plugin-svelte's built-in TS preprocessing. style:false
  // skips CSS preprocessing: the components use plain CSS (no @import, no CSS
  // url() assets - the Svelte compiler scopes it), so the style step is a no-op
  // for the build AND lets vitest compile components without vite 6's
  // preprocessCSS environment (which throws under the test runner).
  preprocess: vitePreprocess({ style: false })
}
