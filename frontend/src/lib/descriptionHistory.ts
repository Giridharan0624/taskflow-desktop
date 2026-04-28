/**
 * Persistent ring-buffer of recent task descriptions.
 *
 * UX contract: when the user focuses the description input, we offer
 * up to 5 of their most recently used descriptions as autocomplete
 * suggestions. Picking one fills the input; typing filters the list.
 *
 * Storage: localStorage — small (~1 KB), never leaves the user's
 * machine, scoped per OS user. We deliberately don't sync to the
 * backend; descriptions are personal scratch and the server doesn't
 * need a full history.
 */

const KEY = "taskflow.descriptionHistory.v1";
const MAX = 25; // Soft cap; we still only display the top 5.

export function loadHistory(): string[] {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return [];
    const arr = JSON.parse(raw);
    if (!Array.isArray(arr)) return [];
    return arr.filter((s) => typeof s === "string");
  } catch {
    return [];
  }
}

/**
 * Record a description. Most-recently-used first; case-insensitive
 * dedupe so "Auth refactor" and "auth refactor" don't both occupy
 * slots. Empty / whitespace-only inputs are ignored.
 */
export function recordHistory(description: string): void {
  const trimmed = description.trim();
  if (!trimmed) return;
  try {
    const existing = loadHistory();
    const lower = trimmed.toLowerCase();
    // Drop any prior entry that case-insensitively matches — this is
    // the dedupe step. Prepend the new one as MRU.
    const next = [
      trimmed,
      ...existing.filter((s) => s.toLowerCase() !== lower),
    ].slice(0, MAX);
    localStorage.setItem(KEY, JSON.stringify(next));
  } catch {
    // localStorage quota / disabled — silently swallow. The feature
    // is purely a convenience.
  }
}

/**
 * Returns up to `limit` recent descriptions whose start matches
 * `query` case-insensitively. Empty query returns the most-recent N
 * unfiltered. Already deduped at write time so this never returns
 * duplicates.
 */
export function suggestHistory(query: string, limit = 5): string[] {
  const all = loadHistory();
  const trimmed = query.trim().toLowerCase();
  if (!trimmed) return all.slice(0, limit);
  return all
    .filter((s) => s.toLowerCase().includes(trimmed))
    .slice(0, limit);
}
