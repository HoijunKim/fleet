// Motion helpers. Svelte's JS-driven transitions do not honor the CSS
// prefers-reduced-motion guard, so gate them here.

export function reducedMotion(): boolean {
  return typeof window !== "undefined" && !!window.matchMedia
    ? window.matchMedia("(prefers-reduced-motion: reduce)").matches
    : false;
}

// flyUp returns fly params for a staggered list entrance, collapsed to an
// instant no-op when the user prefers reduced motion.
export function flyUp(i: number): { y: number; duration: number; delay: number } {
  if (reducedMotion()) return { y: 0, duration: 0, delay: 0 };
  return { y: 6, duration: 180, delay: Math.min(i, 8) * 20 };
}

// fadeScaleIn returns params for svelte's `scale` transition (a soft fade +
// subtle scale-up), collapsed to instant under reduced motion. Use for
// overlays/panels: `transition:scale={fadeScaleIn()}`.
export function fadeScaleIn(): { duration: number; start: number; opacity: number } {
  if (reducedMotion()) return { duration: 0, start: 1, opacity: 1 };
  return { duration: 180, start: 0.98, opacity: 0 };
}
