import { describe, it, expect, vi, beforeEach } from "vitest";

const saved: Record<string, unknown> = {};
vi.mock("../../wailsjs/go/main/App", () => ({
  SaveBrief: vi.fn(async (text: string, at: string, lang: string) => { saved["brief"] = { text, at, lang }; return ""; }),
  SaveChat: vi.fn(async (path: string, turns: unknown) => { saved["chat:" + path] = turns; return ""; }),
  // Report nothing already in the store, so migration writes.
  GetChat: vi.fn(async () => []),
}));

import { migrateIntel } from "./intelMigrate";
import { SaveBrief, SaveChat } from "../../wailsjs/go/main/App";

function fakeLocalStorage(seed: Record<string, string>) {
  const m = new Map(Object.entries(seed));
  return {
    getItem: (k: string) => (m.has(k) ? m.get(k)! : null),
    setItem: (k: string, v: string) => void m.set(k, v),
    removeItem: (k: string) => void m.delete(k),
    key: (i: number) => [...m.keys()][i] ?? null,
    get length() { return m.size; },
    _map: m,
  };
}

describe("migrateIntel", () => {
  beforeEach(() => { for (const k of Object.keys(saved)) delete saved[k]; vi.clearAllMocks(); });

  it("copies brief and chats into the store, then clears the flag-guarded keys", async () => {
    const ls = fakeLocalStorage({
      "fleet.brief": JSON.stringify({ text: "hi", at: "t" }),
      "fleet.briefLang": "ko",
      "fleet.chat:/a/b": JSON.stringify([{ role: "user", text: "q" }]),
      "fleet.chat:__fleet__": JSON.stringify([{ role: "assistant", text: "a" }]),
    });
    (globalThis as any).localStorage = ls;

    await migrateIntel();

    expect(SaveBrief).toHaveBeenCalledWith("hi", "t", "ko");
    expect(SaveChat).toHaveBeenCalledWith("/a/b", [{ role: "user", text: "q" }]);
    expect(SaveChat).toHaveBeenCalledWith("__fleet__", [{ role: "assistant", text: "a" }]);
    expect(ls._map.has("fleet.chat:/a/b")).toBe(false); // migrated key removed
    expect(ls._map.get("fleet.intelMigrated")).toBe("1");
  });

  it("is a no-op on the second run", async () => {
    const ls = fakeLocalStorage({ "fleet.intelMigrated": "1", "fleet.brief": JSON.stringify({ text: "x" }) });
    (globalThis as any).localStorage = ls;
    await migrateIntel();
    expect(SaveBrief).not.toHaveBeenCalled();
  });
});
