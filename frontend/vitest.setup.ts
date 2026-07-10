// vitest runs in the node env (no `window`). `reducedMotion()` gates on
// `typeof window !== "undefined" && window.matchMedia`, and `vi.stubGlobal`
// only stubs globalThis — so without aliasing window to global, the
// reduced-motion tests would silently see reducedMotion()===false. Do not
// remove: it backfills window ONLY when absent (won't clobber a real DOM).
if (typeof window === 'undefined') {
  (global as any).window = global;
}
