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
    S.pending.set({ id: "act-9", toolName: "Edit", toolInput: "{}", category: "edit", severity: "low", summary: "" });
    expect(render()).toContain("Edit");
  });

  it("renders the category badge and summary", () => {
    S.setProject({ path: "/r", repoPath: "/r", name: "r" });
    S.overlayOpen.set(true); S.consent.set(true);
    S.pending.set({ id: "act-10", toolName: "Bash", toolInput: JSON.stringify({ command: "git push origin main" }), category: "remote", severity: "high", summary: "Push branch main to origin" });
    const html = render();
    expect(html).toContain("ov-cat-remote");
    expect(html).toContain("Push branch main to origin");
    expect(html).toContain("git push origin main");
  });

  it("renders a red/green diff for an Edit action", () => {
    S.setProject({ path: "/r", repoPath: "/r", name: "r" });
    S.overlayOpen.set(true); S.consent.set(true);
    S.pending.set({
      id: "act-11", toolName: "Edit", category: "edit", severity: "low", summary: "Edit foo.ts",
      toolInput: JSON.stringify({ file_path: "foo.ts", old_string: "old", new_string: "new" }),
    });
    const html = render();
    expect(html).toContain("foo.ts");
    expect(html).toContain("ov-del");
    expect(html).toContain("ov-add");
  });

  it("renders an approved outcome line with the check icon", () => {
    S.setProject({ path: "/r", repoPath: "/r", name: "r" });
    S.overlayOpen.set(true); S.consent.set(true);
    S.activity.set([{ tool: "approved", input: "Edit foo.ts" }]);
    const html = render();
    expect(html).toContain("ov-act-ok");
    expect(html).toContain("Edit foo.ts");
    S.activity.set([]);
  });

  it("shows the fleet header and read-scope consent copy in fleet mode", () => {
    S.openFleetOverlay();
    S.available.set(true); S.consent.set(false);
    const html = render({ projectName: "All projects" });
    expect(html).toContain("All projects - agentic deep-dive");
    expect(html).toContain("across ALL your projects");
    expect(html).not.toContain("read files in this repo");
  });

  it("shows the single-repo header and consent copy outside fleet mode", () => {
    S.setProject({ path: "/r", repoPath: "/r", name: "r" });
    S.overlayOpen.set(true);
    S.available.set(true); S.consent.set(false);
    const html = render({ projectName: "r" });
    expect(html).toContain("r · agentic deep-dive");
    expect(html).toContain("read files in this repo");
    expect(html).not.toContain("across ALL your projects");
  });
});
