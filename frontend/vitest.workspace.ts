// Two vitest projects: 'ssr' (node, SSR render - the bulk of the suite) and
// 'dom' (happy-dom, browser build - the click-wiring interaction tests). A
// single `vitest run` executes both. They are separate configs because the two
// Svelte compilation modes (server vs client) cannot share one config.
export default ['./vitest.config.ts', './vitest.dom.config.ts']
