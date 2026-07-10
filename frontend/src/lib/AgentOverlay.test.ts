import { describe, it, expect, vi, beforeEach } from "vitest";

const handlers: Record<string, (d: any) => void> = {};
vi.mock("../../wailsjs/runtime/runtime", () => ({
  EventsOn: (n: string, cb: (d: any) => void) => { handlers[n] = cb; return () => {}; },
}));
const calls: any[] = [];
vi.mock("../../wailsjs/go/main/App", () => ({
  AgentAvailable: async () => true, AgentConsent: async () => true,
  GiveAgentConsent: async () => "", AgentAsk: async () => "",
  ApproveAction: async (id: string, ok: boolean) => { calls.push([id, ok]); },
  CancelAgent: () => calls.push(["cancel"]),
}));

import { get } from "svelte/store";
import * as S from "./agentSession";
import AgentOverlay from "./AgentOverlay.svelte";

const render = (props: Record<string, unknown> = {}) => (AgentOverlay as any).render(props).html as string;

beforeEach(() => { calls.length = 0; S.overlayOpen.set(false); S.pending.set(null); });

describe("AgentOverlay", () => {
  it("renders nothing when closed, the panel when open", () => {
    expect(render()).not.toContain("agentic deep-dive");
    S.setProject({ path: "/r", repoPath: "/r", name: "r" });
    S.overlayOpen.set(true); S.consent.set(true);
    expect(render()).toContain("agentic deep-dive");
  });

  it("shows the approval card with the pending tool", () => {
    S.setProject({ path: "/r", repoPath: "/r", name: "r" });
    S.overlayOpen.set(true); S.consent.set(true);
    S.pending.set({ id: "act-9", toolName: "Edit", toolInput: "{}" });
    expect(render()).toContain("Edit");
  });
});
