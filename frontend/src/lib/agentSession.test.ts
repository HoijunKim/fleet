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
  ApproveAction: async (id: string, ok: boolean) => { calls.push(["approve", id, ok]); },
  CancelAgent: () => { calls.push(["cancel"]); },
}));

import * as S from "./agentSession";

// vitest runs in the `node` environment (no localStorage). The store guards
// `typeof localStorage === "undefined"`, so chat persistence simply no-ops here;
// guard the test's own cleanup the same way.
beforeEach(() => { calls.length = 0; if (typeof localStorage !== "undefined") localStorage.clear(); });

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
});
