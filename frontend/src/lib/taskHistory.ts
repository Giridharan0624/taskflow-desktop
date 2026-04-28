/**
 * Per-task usage frequency. Used by the task dropdown to surface
 * the user's most-used tasks at the top — most users alternate
 * between 3-5 tasks and shouldn't have to scroll the dropdown
 * every time.
 *
 * Storage shape (localStorage):
 *
 *   { [taskId]: { count: number, lastUsed: number /epoch ms/ } }
 *
 * Counts decay implicitly via the `lastUsed` filter — we only
 * surface tasks used in the past 7 days. Older entries stay in
 * storage but don't pin to the top.
 */

const KEY = "taskflow.taskHistory.v1";
const RECENT_WINDOW_MS = 7 * 24 * 60 * 60 * 1000; // 7 days

interface Entry {
  count: number;
  lastUsed: number;
}

function load(): Record<string, Entry> {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return {};
    const v = JSON.parse(raw);
    return typeof v === "object" && v !== null ? v : {};
  } catch {
    return {};
  }
}

function save(map: Record<string, Entry>): void {
  try {
    localStorage.setItem(KEY, JSON.stringify(map));
  } catch {
    // ignore quota / disabled
  }
}

/** Increment the use-count for a task. Called from handleStart on
 *  successful timer start. No-op for missing taskId (Meeting / ad-hoc). */
export function recordTaskUse(taskId: string | null | undefined): void {
  if (!taskId) return;
  const map = load();
  const prev = map[taskId];
  map[taskId] = {
    count: (prev?.count ?? 0) + 1,
    lastUsed: Date.now(),
  };
  save(map);
}

/** Return up to N task IDs most-used in the past 7 days, ranked by
 *  count desc then lastUsed desc. Used by TaskSelector to pin
 *  these to the top of the task dropdown. */
export function topRecentTaskIds(limit = 5): string[] {
  const map = load();
  const cutoff = Date.now() - RECENT_WINDOW_MS;
  return Object.entries(map)
    .filter(([, v]) => v.lastUsed >= cutoff)
    .sort((a, b) => {
      if (b[1].count !== a[1].count) return b[1].count - a[1].count;
      return b[1].lastUsed - a[1].lastUsed;
    })
    .slice(0, limit)
    .map(([k]) => k);
}
