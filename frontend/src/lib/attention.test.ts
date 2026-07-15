import { describe, it, expect } from "vitest";
import { deriveAttention, type GHSignal } from "./attention";

// iso returns a YYYY-MM-DD string offset by n days from today (daysUntil reads
// the real clock, so tests use offsets rather than fixed dates).
function iso(n: number): string {
  const d = new Date();
  d.setHours(0, 0, 0, 0);
  d.setDate(d.getDate() + n);
  return d.toISOString().slice(0, 10);
}

// base is a clean, loaded code repo that should produce NO attention item.
function base(over: Record<string, any> = {}) {
  return {
    id: "/r/proj",
    name: "proj",
    type: "code",
    isGit: true,
    loaded: true,
    path: "/r/proj",
    repoPath: "/r/proj",
    remote: "git@github.com:me/proj.git",
    branch: "feat/x",
    dirty: false,
    modified: 0,
    ahead: 0,
    hasUpstream: true,
    lastWhen: iso(-1),
    todo: 0,
    deadline: "",
    errMsg: "",
    ...over,
  };
}

describe("deriveAttention", () => {
  it("clean on-track project produces no item", () => {
    expect(deriveAttention([base()], new Map())).toEqual([]);
  });

  it("skips non-code, not-loaded, errored, non-git projects", () => {
    const bad = [
      base({ type: "manual", dirty: true }),
      base({ loaded: false, dirty: true }),
      base({ errMsg: "boom", dirty: true }),
      base({ isGit: false, dirty: true }),
    ];
    expect(deriveAttention(bad, new Map())).toEqual([]);
  });

  it("flags dirty, unpushed, idle, todo from git fields", () => {
    const dirty = deriveAttention([base({ dirty: true, modified: 3 })], new Map());
    expect(dirty[0].reasons.map((r) => r.kind)).toContain("dirty");
    expect(dirty[0].actions).toContain("editor");

    const unpushed = deriveAttention([base({ ahead: 2, hasUpstream: true })], new Map());
    expect(unpushed[0].reasons.map((r) => r.kind)).toContain("unpushed");
    expect(unpushed[0].actions).toContain("push");

    const idle = deriveAttention([base({ lastWhen: iso(-20) })], new Map());
    expect(idle[0].reasons.map((r) => r.kind)).toContain("idle");

    const todo = deriveAttention([base({ todo: 12 })], new Map());
    expect(todo[0].reasons.map((r) => r.kind)).toContain("todo");
  });

  it("push action requires ahead>0 AND hasUpstream", () => {
    const noUpstream = deriveAttention([base({ ahead: 2, hasUpstream: false })], new Map());
    // ahead without upstream -> no unpushed reason, no push action
    expect(noUpstream).toEqual([]);
  });

  it("derives CI-failing and open-PR reasons from GitHub signals", () => {
    const gh = new Map<string, GHSignal>([["/r/proj", { ci: "failure", prs: 2, issues: 0 }]]);
    const items = deriveAttention([base()], gh);
    const kinds = items[0].reasons.map((r) => r.kind);
    expect(kinds).toContain("ci");
    expect(kinds).toContain("prs");
    expect(items[0].actions).toContain("ci");
    expect(items[0].actions).toContain("prs");
  });

  it("omits ci/prs reasons when GitHub signals are absent", () => {
    const items = deriveAttention([base({ dirty: true })], new Map());
    const kinds = items[0].reasons.map((r) => r.kind);
    expect(kinds).not.toContain("ci");
    expect(kinds).not.toContain("prs");
  });

  it("deadline near or overdue is flagged high, far deadline is not", () => {
    expect(deriveAttention([base({ deadline: iso(2) })], new Map())[0].reasons[0].kind).toBe("deadline");
    expect(deriveAttention([base({ deadline: iso(-1) })], new Map())[0].reasons[0].label).toContain("overdue");
    expect(deriveAttention([base({ deadline: iso(30) })], new Map())).toEqual([]);
  });

  it("merges multiple reasons into one row per project (dedup)", () => {
    const gh = new Map<string, GHSignal>([["/r/proj", { ci: "failure", prs: 1, issues: 0 }]]);
    const items = deriveAttention([base({ dirty: true, ahead: 1, hasUpstream: true })], gh);
    expect(items).toHaveLength(1);
    expect(items[0].reasons.length).toBeGreaterThanOrEqual(3);
    expect(items[0].actions.length).toBeLessThanOrEqual(3);
  });

  it("ranks CI-failing / overdue-deadline above idle/todo", () => {
    const gh = new Map<string, GHSignal>([["/a", { ci: "failure", prs: 0, issues: 0 }]]);
    const projects = [
      base({ id: "/idle", name: "idle-one", repoPath: "/idle", path: "/idle", lastWhen: iso(-20) }),
      base({ id: "/a", name: "ci-one", repoPath: "/a", path: "/a" }),
    ];
    const items = deriveAttention(projects, gh);
    expect(items[0].name).toBe("ci-one");
  });

  it("ranks a single high-severity reason above a stack of med/low", () => {
    const gh = new Map<string, GHSignal>([
      ["/ci", { ci: "failure", prs: 0, issues: 0 }],
      ["/stack", { ci: "success", prs: 3, issues: 0 }],
    ]);
    const projects = [
      base({ id: "/stack", name: "stack", repoPath: "/stack", path: "/stack", dirty: true, modified: 1, ahead: 1, hasUpstream: true }),
      base({ id: "/ci", name: "ci-one", repoPath: "/ci", path: "/ci" }),
    ];
    const items = deriveAttention(projects, gh);
    expect(items[0].name).toBe("ci-one"); // lone high beats dirty+unpushed+prs stack
  });

  it("always includes a dig-in action (ask or open) on a busy row", () => {
    const gh = new Map<string, GHSignal>([["/r/proj", { ci: "failure", prs: 2, issues: 0 }]]);
    const acts = deriveAttention([base({ dirty: true, todo: 12 })], gh)[0].actions;
    expect(acts.length).toBeLessThanOrEqual(3);
    expect(acts.includes("ask") || acts.includes("open")).toBe(true);
  });

  it("a deadline row's dig-in is 'open'", () => {
    expect(deriveAttention([base({ deadline: iso(1) })], new Map())[0].actions).toContain("open");
  });

  it("returns all ranked items (display cap is the caller's job)", () => {
    const many = Array.from({ length: 20 }, (_, i) =>
      base({ id: "/p" + i, name: "p" + i, repoPath: "/p" + i, path: "/p" + i, todo: 12 }),
    );
    expect(deriveAttention(many, new Map()).length).toBe(20);
  });
});
