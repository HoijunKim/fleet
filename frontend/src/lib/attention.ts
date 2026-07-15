// deriveAttention turns loaded projects + GitHub signals into a ranked list of
// items that need attention today, each carrying the contextual one-click
// actions relevant to its reasons. Pure and testable: no Svelte, no bindings.

import { daysUntil } from "./pm";
import { ciBit } from "./ciBit";

export type ReasonKind = "ci" | "deadline" | "unpushed" | "dirty" | "prs" | "idle" | "todo";
export type ActionKind = "editor" | "push" | "ci" | "prs" | "ask" | "open";
export type Severity = "high" | "med" | "low";

export interface Reason {
  kind: ReasonKind;
  label: string;
  sev: Severity;
}

export interface AttentionItem {
  id: string;
  name: string;
  path: string;
  repoPath: string;
  remote: string;
  branch: string;
  reasons: Reason[];
  actions: ActionKind[];
  rank: number;
}

export interface GHSignal {
  ci: string;
  prs: number;
  issues: number;
}

const IDLE_DAYS = 14;
const DEADLINE_NEAR_DAYS = 3;

const SEV_ORDER: Record<Severity, number> = { high: 3, med: 2, low: 1 };
const SEV_WEIGHT: Record<Severity, number> = { high: 100, med: 40, low: 10 };
const KIND_WEIGHT: Record<ReasonKind, number> = {
  ci: 60, deadline: 55, unpushed: 30, dirty: 25, prs: 20, idle: 8, todo: 6,
};

function daysSince(when: string): number | null {
  const d = daysUntil(when);
  return d === null ? null : -d;
}

// actionsFor maps a row's reasons to a deduped set of one-click actions: up to
// two contextual "signal" actions plus one guaranteed dig-in (open for a
// deadline row, else ask), so a busy row never loses its way to investigate.
// Push appears only for an unpushed reason (which already implies ahead>0 &&
// hasUpstream). GitHub actions (ci/prs) require a remote - and a ci/prs reason
// only exists when GitHub signals loaded, i.e. a GitHub remote.
function actionsFor(reasons: Reason[], p: any): ActionKind[] {
  const kinds = new Set(reasons.map((r) => r.kind));
  const hasRemote = !!p.remote;
  const signals: ActionKind[] = [];
  const add = (a: ActionKind) => {
    if (!signals.includes(a)) signals.push(a);
  };

  if (kinds.has("ci") && hasRemote) add("ci");
  if (kinds.has("prs") && hasRemote) add("prs");
  if (kinds.has("unpushed")) add("push");
  if (kinds.has("dirty") || kinds.has("idle") || kinds.has("todo") || kinds.has("unpushed")) add("editor");

  const digIn: ActionKind = kinds.has("deadline") ? "open" : "ask";
  return [...signals.slice(0, 2), digIn];
}

export function deriveAttention(
  projects: any[],
  ghByPath?: Map<string, GHSignal>,
): AttentionItem[] {
  const gh = ghByPath || new Map<string, GHSignal>();
  const items: AttentionItem[] = [];

  for (const p of projects || []) {
    if (p.type !== "code" || !p.isGit || !p.loaded || p.errMsg) continue;
    const reasons: Reason[] = [];
    const g = gh.get(p.repoPath || p.path);

    if (p.deadline) {
      const d = daysUntil(p.deadline);
      if (d !== null && d <= DEADLINE_NEAR_DAYS) {
        reasons.push({
          kind: "deadline",
          label: d < 0 ? -d + "d overdue" : d === 0 ? "due today" : "due in " + d + "d",
          sev: "high",
        });
      }
    }
    if (g && ciBit(g.ci) === "CI failing") {
      reasons.push({ kind: "ci", label: "CI failing", sev: "high" });
    }
    if (p.ahead > 0 && p.hasUpstream) {
      reasons.push({ kind: "unpushed", label: p.ahead + " to push", sev: "med" });
    }
    if (p.dirty) {
      reasons.push({ kind: "dirty", label: (p.modified || 0) + " uncommitted", sev: "med" });
    }
    if (g && g.prs > 0) {
      reasons.push({ kind: "prs", label: g.prs + " open PR" + (g.prs === 1 ? "" : "s"), sev: "low" });
    }
    const since = daysSince(p.lastWhen);
    if (!p.dirty && !(p.ahead > 0) && since !== null && since > IDLE_DAYS) {
      reasons.push({ kind: "idle", label: "untouched " + since + "d", sev: "low" });
    }
    if ((p.todo || 0) >= 8) {
      reasons.push({ kind: "todo", label: p.todo + " TODOs", sev: "low" });
    }

    if (reasons.length === 0) continue;

    // Rank primarily by the most-severe reason (a single high beats any stack of
    // med/low), with the summed weight only breaking ties within a bucket.
    const maxSev = Math.max(...reasons.map((r) => SEV_ORDER[r.sev]));
    const weight = reasons.reduce((sum, r) => sum + SEV_WEIGHT[r.sev] + KIND_WEIGHT[r.kind], 0);
    const rank = maxSev * 100000 + weight;
    items.push({
      id: p.id,
      name: p.name,
      path: p.path || p.repoPath,
      repoPath: p.repoPath || p.path,
      remote: p.remote || "",
      branch: p.branch || "",
      reasons,
      actions: actionsFor(reasons, p),
      rank,
    });
  }

  // Return ALL ranked items; the caller decides how many to display and shows a
  // "+N more" affordance for the rest, so a fleet with many problems never looks
  // fully handled.
  items.sort((a, b) => b.rank - a.rank);
  return items;
}
