import { writable, get } from "svelte/store";
import {
  AgentAvailable, AgentConsent, GiveAgentConsent, AgentAsk, AgentAskFleet, ApproveAction, CancelAgent,
} from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";

type Turn = { role: "user" | "assistant"; text: string };
type Proj = { path: string; repoPath?: string; name?: string; isFleet?: boolean } | null;

export const available = writable(false);
export const consent = writable(false);
export const running = writable(false);
export const stream = writable("");
export const activity = writable<{ tool: string; input: string }[]>([]);
export const pending = writable<{
  id: string; toolName: string; toolInput: string;
  category: string; severity: string; summary: string;
} | null>(null);
export const cost = writable<{ costUsd: number; inputTokens: number; outputTokens: number } | null>(null);
export const turns = writable<Turn[]>([]);
export const overlayOpen = writable(false);
// Whether the CURRENT identity (as scoped by setProject/openOverlay) is the
// fleet-wide "__fleet__" identity, not a single repo. Consumers (e.g.
// AgentOverlay) read this to switch header/consent copy without reaching into
// module-private state.
export const isFleet = writable(false);

let project: Proj = null;
let loadedPath = "";
let gen = 0;        // bumped on cancel/switch; late events with a stale gen are dropped
let runPath = "";
let runGen = 0;
let deciding = false;
let started = false;

function stale(): boolean {
  return !project || runPath !== project.path || runGen !== gen;
}
function fmtInput(v: any): string {
  if (typeof v === "string") return v;
  try { return JSON.stringify(v, null, 2); } catch { return String(v); }
}
function chatKey(p: string) { return "fleet.chat:" + p; }
function loadChat(p: string): Turn[] {
  if (typeof localStorage === "undefined") return [];
  try {
    const r = localStorage.getItem(chatKey(p)); const a = r ? JSON.parse(r) : [];
    return Array.isArray(a) ? a.filter((t) => t && (t.role === "user" || t.role === "assistant")) : [];
  }
  catch { return []; }
}
function saveChat() {
  if (typeof localStorage === "undefined" || !loadedPath) return;
  try { localStorage.setItem(chatKey(loadedPath), JSON.stringify(get(turns).slice(-20))); } catch { /* non-fatal */ }
}

export async function initAgentSession(): Promise<void> {
  if (!started) {
    started = true;
    EventsOn("agent:text", (t: any) => { if (stale()) return; stream.update((s) => s + String(t ?? "")); });
    EventsOn("agent:activity", (a: any) => { if (stale()) return; activity.update((x) => [...x, { tool: a?.tool ?? "", input: fmtInput(a?.input) }]); });
    EventsOn("agent:action", (a: any) => {
      if (stale()) return;
      pending.set({
        id: a?.id ?? "", toolName: a?.toolName ?? "", toolInput: fmtInput(a?.toolInput),
        category: a?.category ?? "shell", severity: a?.severity ?? "medium", summary: a?.summary ?? "",
      });
    });
    EventsOn("agent:done", (d: any) => {
      if (stale()) return;
      cost.set({ costUsd: d?.costUsd ?? 0, inputTokens: d?.inputTokens ?? 0, outputTokens: d?.outputTokens ?? 0 });
      const answer = get(stream).trim() || String(d?.result ?? "(no answer)");
      turns.update((t) => [...t, { role: "assistant", text: answer }]); saveChat();
      stream.set(""); activity.set([]); pending.set(null); running.set(false);
    });
    EventsOn("agent:error", (e: any) => {
      if (stale()) return;
      turns.update((t) => [...t, { role: "assistant", text: "error: " + String(e ?? "agent failed") }]); saveChat();
      stream.set(""); pending.set(null); running.set(false);
    });
  }
  try { available.set(await AgentAvailable()); consent.set(await AgentConsent()); }
  catch { available.set(false); }
}

export function setProject(p: Proj): void {
  // Same-path re-entrancy guard only (e.g. re-selecting the already-scoped
  // repo). NOT a fleet-vs-repo guard: a repo path never equals "__fleet__",
  // so this does nothing to protect a live fleet run from being clobbered by
  // a subsequent setProject(repo) call - callers must not auto-scope while
  // isFleet is true (see App.svelte's `$: if (!$isFleet) agentSetProject(...)`).
  if (p && project && p.path === project.path) return;
  if (get(running)) { CancelAgent(); gen++; running.set(false); pending.set(null); activity.set([]); stream.set(""); cost.set(null); }
  overlayOpen.set(false);
  project = p;
  loadedPath = p ? p.path : "";
  turns.set(p ? loadChat(p.path) : []);
  isFleet.set(!!(p && p.isFleet));
}

export async function giveConsent(): Promise<string> {
  const msg = await GiveAgentConsent();
  if (!msg) consent.set(true);
  return msg || "";
}

export async function ask(q: string): Promise<void> {
  const text = q.trim();
  if (!text || !project || get(running)) return;
  turns.update((t) => [...t, { role: "user", text }]);
  stream.set(""); activity.set([]); pending.set(null); cost.set(null); running.set(true);
  runPath = project.path; runGen = ++gen;
  const err = project.isFleet
    ? await AgentAskFleet(text)
    : await AgentAsk(project.repoPath || project.path, text);
  if (stale()) return;
  if (err) { turns.update((t) => [...t, { role: "assistant", text: err }]); running.set(false); }
}

export async function decide(approved: boolean): Promise<void> {
  const p = get(pending);
  if (!p || deciding) return;
  const summary = p.summary;
  pending.set(null);
  activity.update((x) => [...x, { tool: approved ? "approved" : "rejected", input: summary }]);
  deciding = true;
  try { await ApproveAction(p.id, approved); } finally { deciding = false; }
}

export function cancel(): void {
  CancelAgent(); gen++; running.set(false); pending.set(null);
}

export function openOverlay(p: Proj): void {
  setProject(p);
  // A single-shot session may have written fleet.chat:<path> since this repo
  // was first scoped; reload so an agentic saveChat() can't clobber it. Skip
  // while a run is live (its in-memory turns aren't on disk yet).
  if (p && !get(running)) { loadedPath = p.path; turns.set(loadChat(p.path)); }
  overlayOpen.set(true);
}
export function closeOverlay(): void { overlayOpen.set(false); } // does NOT cancel the run

// Fleet identity: scopes the shared session to a synthetic "__fleet__" path so
// cancel-on-switch + staleness work exactly like any other project, but ask()
// dispatches to AgentAskFleet (agentic run at the projects root) instead of a
// single repo.
export function openFleetOverlay(): void {
  openOverlay({ path: "__fleet__", name: "All projects", isFleet: true });
}

// Test-isolation only: resets every store and module-level run state to its
// initial value. Does NOT reset `started` - the agent:* event subscriptions
// stay registered across resets within a single test process.
export function __reset(): void {
  available.set(false);
  consent.set(false);
  running.set(false);
  stream.set("");
  activity.set([]);
  pending.set(null);
  cost.set(null);
  turns.set([]);
  overlayOpen.set(false);
  isFleet.set(false);
  project = null;
  loadedPath = "";
  gen = 0;
  runPath = "";
  runGen = 0;
  deciding = false;
}
