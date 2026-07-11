export type RepoChip = { repo: string; count: number };

// deriveChips lists repos present in hits, in first-seen order, with counts.
export function deriveChips(hits: { repo: string }[]): RepoChip[] {
  const order: string[] = [];
  const counts = new Map<string, number>();
  for (const h of hits) {
    if (!counts.has(h.repo)) order.push(h.repo);
    counts.set(h.repo, (counts.get(h.repo) ?? 0) + 1);
  }
  return order.map((repo) => ({ repo, count: counts.get(repo) ?? 0 }));
}

// visibleIndices returns the flat indices of hits whose repo is not hidden.
export function visibleIndices(hits: { repo: string }[], hidden: Set<string>): number[] {
  const out: number[] = [];
  hits.forEach((h, i) => { if (!hidden.has(h.repo)) out.push(i); });
  return out;
}

// clampSel returns the visible index nearest to sel (or 0 when none visible).
export function clampSel(sel: number, visible: number[]): number {
  if (visible.length === 0) return 0;
  if (visible.includes(sel)) return sel;
  let best = visible[0], bestDist = Math.abs(visible[0] - sel);
  for (const v of visible) {
    const d = Math.abs(v - sel);
    if (d < bestDist) { best = v; bestDist = d; }
  }
  return best;
}
