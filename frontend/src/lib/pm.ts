// Shared project-management helpers used by the project list and detail panel.
// Deadlines are stored as free-form strings; the UI writes plain "YYYY-MM-DD".

// Order used when sorting by project status (active first, done last).
export const STATUS_ORDER: Record<string, number> = { active: 0, paused: 1, done: 2 };

// Number of whole days from today until the deadline (negative = overdue).
// Returns null when the deadline is empty or cannot be parsed.
export function daysUntil(deadline: string): number | null {
  if (!deadline) return null;
  const d = new Date(deadline.length <= 10 ? deadline + "T00:00:00" : deadline);
  if (isNaN(d.getTime())) return null;
  const today = new Date();
  today.setHours(0, 0, 0, 0);
  return Math.round((d.getTime() - today.getTime()) / 86400000);
}

// Countdown label + style class for a deadline, or null when there is none.
export function ddayLabel(deadline: string): { text: string; cls: string } | null {
  const n = daysUntil(deadline);
  if (n === null) return null;
  if (n > 0) return { text: "D-" + n, cls: n <= 3 ? "soon" : "" };
  if (n === 0) return { text: "D-DAY", cls: "soon" };
  return { text: "D+" + -n, cls: "over" };
}

// Sort key for deadlines: sooner dates first, projects with no deadline last.
export function deadlineSort(deadline: string): number {
  const n = daysUntil(deadline);
  return n === null ? Number.POSITIVE_INFINITY : n;
}
