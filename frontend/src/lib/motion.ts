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
