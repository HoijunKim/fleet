// sortPref persists the project-table sort (key + direction) to localStorage so
// it survives a restart. It is a per-device view preference - deliberately NOT
// in the synced store, so a user can sort differently on each machine.

export type SortDir = "asc" | "desc";
export type SortPref = { key: string; dir: SortDir };

const KEY = "fleet.sort";
const DEFAULT: SortPref = { key: "", dir: "asc" };

export function loadSortPref(): SortPref {
  if (typeof localStorage === "undefined") return { ...DEFAULT };
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return { ...DEFAULT };
    const o = JSON.parse(raw);
    const key = typeof o?.key === "string" ? o.key : "";
    const dir: SortDir = o?.dir === "desc" ? "desc" : "asc";
    return { key, dir };
  } catch {
    return { ...DEFAULT };
  }
}

export function saveSortPref(key: string, dir: SortDir): void {
  if (typeof localStorage === "undefined") return;
  try {
    localStorage.setItem(KEY, JSON.stringify({ key, dir }));
  } catch {
    /* quota - non-fatal */
  }
}
