// @vitest-environment happy-dom
//
// DOM tests of the Settings data-management click wiring that SSR cannot reach:
// the Restore button (tier 4f) and the Import flow (tier 4g). Mounts the real
// modal, waits for the config-gated General tab, and drives the buttons.
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/svelte";

const calls: any[] = [];
vi.mock("../../wailsjs/go/main/App", () => {
  const ok = async () => "";
  return {
    GetConfig: async () => ({
      Roots: ["/projects"], ScanDepth: 2, Editor: "code", Terminal: "wt",
      AutoFetchMinutes: 0, ShowNonGit: true, AIProvider: "claude", AIModel: "",
      OpenAIKey: "", GeminiKey: "", NotionToken: "", NotionDatabaseID: "",
    }),
    SaveConfig: ok, AICheck: async () => true, AskAI: ok,
    NotionDatabases: async () => [], DetectEditors: async () => [],
    ExportData: ok, DirExists: async () => true, RevealDataDir: ok,
    BuildVersion: async () => "dev",
    ConflictBackups: async () => [{ localId: "m-1", name: "lost project", when: "2026-07-01T00:00:00Z" }],
    RestoreBackup: async (localId: string, when: string) => { calls.push(["restore", localId, when]); return ""; },
    ImportPreview: async () => { calls.push(["preview"]); return { path: "/tmp/x.json", projects: 3, projectsOverwrite: 1, chats: 2, chatsOverwrite: 0, brief: true, error: "" }; },
    ImportCommit: async (path: string) => { calls.push(["commit", path]); return ""; },
  };
});

import SettingsModal from "./SettingsModal.svelte";

describe("SettingsModal data actions (DOM)", () => {
  beforeEach(() => {
    calls.length = 0;
    // Both flows confirm; auto-accept.
    vi.stubGlobal("confirm", () => true);
  });

  it("restores a backup when its Restore button is clicked", async () => {
    const { findByText } = render(SettingsModal, { props: { onClose: () => {}, onSaved: () => {} } });
    const restore = await findByText("Restore");
    await fireEvent.click(restore);
    // Let the async handler settle.
    await Promise.resolve();
    expect(calls).toContainEqual(["restore", "m-1", "2026-07-01T00:00:00Z"]);
  });

  it("previews then commits an import", async () => {
    const { findByText } = render(SettingsModal, { props: { onClose: () => {}, onSaved: () => {} } });
    const importBtn = await findByText("Import data (JSON)");
    await fireEvent.click(importBtn);
    // preview -> confirm -> commit, each awaited internally.
    await new Promise((r) => setTimeout(r, 0));
    expect(calls).toContainEqual(["preview"]);
    expect(calls).toContainEqual(["commit", "/tmp/x.json"]);
  });
});
