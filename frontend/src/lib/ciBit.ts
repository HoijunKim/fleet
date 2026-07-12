// ciBit maps a GitHub CI conclusion/status to a compact brief bit. Only
// problems/in-flight surface; success/neutral produce no bit (keep lines tight).
export function ciBit(ci: string): string {
  const c = (ci ?? "").trim().toLowerCase();
  if (c === "" || c === "success" || c === "neutral" || c === "skipped") return "";
  if (c === "in_progress" || c === "queued" || c === "pending" || c === "requested" || c === "waiting") return "CI running";
  return "CI failing";
}
