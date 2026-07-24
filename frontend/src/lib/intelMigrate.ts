import { SaveBrief, SaveChat, GetChat } from "../../wailsjs/go/main/App";

// migrateIntel moves brief/chat data out of localStorage into the Go store, once.
// It writes each key only if the store does not already have it (so a re-run
// after a partial failure cannot clobber newer store data), removes the migrated
// localStorage keys, and sets a flag so it never runs again. Any failure leaves
// localStorage intact: the old data is not destroyed until it is safely stored.
const FLAG = "fleet.intelMigrated";

export async function migrateIntel(): Promise<void> {
  if (typeof localStorage === "undefined") return;
  if (localStorage.getItem(FLAG) === "1") return;
  try {
    // Brief: a single global blob plus its language.
    const rawBrief = localStorage.getItem("fleet.brief");
    if (rawBrief) {
      const b = JSON.parse(rawBrief);
      const lang = localStorage.getItem("fleet.briefLang") || "ko";
      if (b && typeof b.text === "string") {
        const err = await SaveBrief(b.text, b.at || "", lang);
        if (err) return; // leave localStorage intact; retry next launch
      }
    }

    // Chats: every fleet.chat:<path> key. The path after the prefix is what the
    // binding maps to an identity, so "__fleet__" and real paths both work.
    const chatKeys: string[] = [];
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i);
      if (k && k.startsWith("fleet.chat:")) chatKeys.push(k);
    }
    for (const k of chatKeys) {
      const path = k.slice("fleet.chat:".length);
      const existing = await GetChat(path);
      if (existing && existing.length > 0) continue; // store already has it
      const turns = JSON.parse(localStorage.getItem(k) || "[]");
      if (Array.isArray(turns) && turns.length > 0) {
        const err = await SaveChat(path, turns);
        if (err) return;
      }
    }

    // Only now, with everything safely stored, remove the old keys.
    localStorage.removeItem("fleet.brief");
    localStorage.removeItem("fleet.briefLang");
    for (const k of chatKeys) localStorage.removeItem(k);
    localStorage.setItem(FLAG, "1");
  } catch {
    // Leave localStorage untouched; the next launch retries.
  }
}
