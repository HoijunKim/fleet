// Normalize a gated agent action into a shape the approval card can render.
// Never throws; unknown/garbage shapes fall back to { kind: "raw" }.
export type Action =
  | { kind: "diff"; file: string; removed: string[]; added: string[] }
  | { kind: "write"; file: string; preview: string[] }
  | { kind: "command"; command: string }
  | { kind: "raw"; json: string };

function asObj(input: any): any | null {
  if (input && typeof input === "object") return input;
  if (typeof input === "string") {
    try { return JSON.parse(input); } catch { return null; }
  }
  return null;
}

export function parseAction(category: string, toolName: string, toolInput: any): Action {
  const o = asObj(toolInput);
  const raw = (): Action => ({ kind: "raw", json: typeof toolInput === "string" ? toolInput : safeJson(toolInput) });
  if (!o) return raw();
  if (toolName === "Edit" && typeof o.old_string === "string" && typeof o.new_string === "string") {
    return { kind: "diff", file: String(o.file_path ?? ""), removed: o.old_string.split("\n"), added: o.new_string.split("\n") };
  }
  if (toolName === "Write" && typeof o.content === "string") {
    return { kind: "write", file: String(o.file_path ?? ""), preview: o.content.split("\n").slice(0, 20) };
  }
  if (toolName === "Bash" && typeof o.command === "string") {
    return { kind: "command", command: o.command };
  }
  return raw();
}

function safeJson(v: any): string {
  try { return JSON.stringify(v, null, 2); } catch { return String(v); }
}
