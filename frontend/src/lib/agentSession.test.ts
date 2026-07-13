import { describe, it, expect, vi, beforeEach } from "vitest";
import { get } from "svelte/store";

// Capture EventsOn handlers so the test can emit agent:* events.
const handlers: Record<string, (d: any) => void> = {};
vi.mock("../../wailsjs/runtime/runtime", () => ({
  EventsOn: (name: string, cb: (d: any) => void) => {
    handlers[name] = cb;
    return () => delete handlers[name];
  },
}));
const calls: any[] = [];
vi.mock("../../wailsjs/go/main/App", () => ({
  AgentAvailable: async () => true,
  AgentConsent: async () => true,
  GiveAgentConsent: async () => "",
  AgentAsk: async (id: string, q: string) => { calls.push(["ask", id, q]); return ""; },
  AgentAskFleet: async (q: string) => { calls.push(["askFleet", q]); return ""; },
  ApproveAction: async (id: string, ok: boolean) => { calls.push(["approve", id, ok]); },
  CancelAgent: () => { calls.push(["cancel"]); },
}));

import * as S from "./agentSession";

// vitest runs in the `node` environment (no localStorage). The store guards
// `typeof localStorage === "undefined"`, so chat persistence simply no-ops here;
// guard the test's own cleanup the same way. __reset() clears module-level run
// state (project/running/gen/...) so a test can't inherit a stuck `running`
// flag left by a prior test that never fired agent:done/agent:error.
beforeEach(() => {
  calls.length = 0;
  if (typeof localStorage !== "undefined") localStorage.clear();
  S.__reset();
});

describe("agentSession", () => {
  it("runs a question and lands the answer on agent:done", async () => {
    await S.initAgentSession();
    S.setProject({ path: "/repo/a", repoPath: "/repo/a", name: "a" });
    await S.ask("what changed?");
    expect(get(S.running)).toBe(true);
    expect(calls[0]).toEqual(["ask", "/repo/a", "what changed?"]);
    handlers["agent:text"]("partial ");
    handlers["agent:done"]({ result: "done", costUsd: 0.01, inputTokens: 5, outputTokens: 9 });
    const turns = get(S.turns);
    expect(turns[turns.length - 1]).toEqual({ role: "assistant", text: "partial" });
    expect(get(S.running)).toBe(false);
  });

  it("ignores stale events after the project switches mid-run", async () => {
    await S.initAgentSession();
    S.setProject({ path: "/repo/a", repoPath: "/repo/a" });
    await S.ask("q");
    S.setProject({ path: "/repo/b", repoPath: "/repo/b" }); // cancels + bumps gen
    expect(calls.some((c) => c[0] === "cancel")).toBe(true);
    handlers["agent:done"]({ result: "leaked", costUsd: 0, inputTokens: 0, outputTokens: 0 });
    expect(get(S.turns).some((t) => t.text === "leaked")).toBe(false);
  });

  it("decide round-trips the pending action id and clears it", async () => {
    await S.initAgentSession();
    S.setProject({ path: "/repo/a", repoPath: "/repo/a" });
    await S.ask("q");
    handlers["agent:action"]({ id: "act-1", toolName: "Edit", toolInput: { file: "x" } });
    expect(get(S.pending)?.id).toBe("act-1");
    await S.decide(true);
    expect(calls).toContainEqual(["approve", "act-1", true]);
    expect(get(S.pending)).toBe(null);
  });

  it("routes ask to AgentAskFleet in fleet mode, AgentAsk otherwise", async () => {
    await S.initAgentSession();
    S.setProject({ path: "/repo/a", repoPath: "/repo/a" });
    await S.ask("q1");
    expect(calls).toContainEqual(["ask", "/repo/a", "q1"]); // AgentAsk
    S.setProject({ path: "__fleet__", name: "All projects", isFleet: true });
    await S.ask("q2");
    expect(calls).toContainEqual(["askFleet", "q2"]); // AgentAskFleet
  });

  it("openFleetOverlay scopes the store to the fleet identity and opens the overlay", async () => {
    await S.initAgentSession();
    S.setProject({ path: "/repo/a", repoPath: "/repo/a" });
    S.openFleetOverlay();
    expect(get(S.overlayOpen)).toBe(true);
    await S.ask("q3");
    expect(calls).toContainEqual(["askFleet", "q3"]);
  });

  it("exposes isFleet so consumers can detect the fleet identity without private state", async () => {
    await S.initAgentSession();
    S.setProject({ path: "/repo/a", repoPath: "/repo/a" });
    expect(get(S.isFleet)).toBe(false);
    S.openFleetOverlay();
    expect(get(S.isFleet)).toBe(true);
    S.setProject({ path: "/repo/a", repoPath: "/repo/a" });
    expect(get(S.isFleet)).toBe(false);
  });

  // Regression for the fleet-clobber bug: App.svelte's `$: agentSetProject(selected)`
  // re-fires on every `projects` refresh (Svelte's safe_not_equal treats the
  // freshly-recomputed `selected` object as dirty even when unchanged), which
  // used to rescope away from the fleet identity and CancelAgent() a live
  // fleet run out from under the user. isFleet must track setProject's
  // current identity in every case so App's `$: if (!$isFleet) agentSetProject(selected)`
  // guard can suppress that re-scoping while fleet mode is active.
  it("tracks the current identity via isFleet across fleet, repo, and null projects", async () => {
    await S.initAgentSession();
    S.setProject({ path: "__fleet__", name: "All projects", isFleet: true });
    expect(get(S.isFleet)).toBe(true);
    S.setProject({ path: "/repo/a", repoPath: "/repo/a" });
    expect(get(S.isFleet)).toBe(false);
    S.setProject(null);
    expect(get(S.isFleet)).toBe(false);
  });
});
