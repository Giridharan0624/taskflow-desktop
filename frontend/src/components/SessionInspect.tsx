import { useEffect } from "preact/hooks";
import type { AttendanceSession } from "../app";
import { cn } from "../lib/cn";

interface SessionInspectProps {
  open: boolean;
  onClose: () => void;
  /** All sessions belonging to the inspected task (we group by task,
   *  so a single row can represent multiple sessions). */
  sessions: AttendanceSession[];
  taskTitle: string;
  projectName: string;
}

/**
 * Bottom-sheet drawer surfacing the per-session breakdown for a row
 * the user right-clicked / long-pressed. No editing yet (that's a
 * future tier of work) — read-only, but lets a power user verify
 * what got tracked when without leaving the desktop app.
 */
export function SessionInspect({
  open,
  onClose,
  sessions,
  taskTitle,
  projectName,
}: SessionInspectProps) {
  // Esc closes — same convention as ShortcutHelp.
  useEffect(() => {
    if (!open) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open) return null;

  const totalMinutes = sessions.reduce((sum, s) => sum + sessionMinutes(s), 0);
  const totalLabel = formatHM(totalMinutes);

  return (
    <div
      class="fixed inset-0 z-40 flex items-end justify-center"
      role="dialog"
      aria-modal="true"
      aria-label={`Session details for ${taskTitle}`}
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      {/* Backdrop */}
      <div
        class="absolute inset-0 bg-foreground/30 supports-[backdrop-filter]:bg-foreground/25 supports-[backdrop-filter]:backdrop-blur-[1.5px]"
        aria-hidden="true"
      />

      {/* Sheet — slides up from the bottom of the window. Caps at
          ~70% viewport height so the user can still see what's
          underneath; scrolls internally for long session lists. */}
      <div
        class={cn(
          "relative w-full max-h-[70vh] flex flex-col",
          "rounded-t-xl border-t border-x border-border bg-popover text-popover-foreground shadow-2xl",
          "animate-in slide-in-from-bottom-2 fade-in",
        )}
      >
        {/* Drag handle decoration — visual cue this is a sheet, not
            a permanent panel. Non-functional (no actual drag). */}
        <div class="flex justify-center pt-2 pb-1" aria-hidden="true">
          <span class="h-1 w-8 rounded-full bg-muted-foreground/30" />
        </div>

        <div class="flex items-start justify-between gap-3 px-4 pb-2 border-b border-border">
          <div class="min-w-0 flex-1">
            <p class="text-[9.5px] font-semibold uppercase tracking-[0.10em] text-muted-foreground/85 leading-none mb-1">
              Session details
            </p>
            <h3 class="text-[14px] font-semibold text-foreground leading-tight truncate" title={taskTitle}>
              {taskTitle}
            </h3>
            {projectName && (
              <p class="text-[11px] text-muted-foreground truncate mt-0.5" title={projectName}>
                {projectName}
              </p>
            )}
          </div>
          <div class="flex items-start gap-2">
            <div class="text-right">
              <p class="text-[9.5px] font-semibold uppercase tracking-[0.10em] text-muted-foreground/85 leading-none">
                Total
              </p>
              <p class="text-[14px] font-bold font-mono tabular-nums text-foreground leading-tight mt-1">
                {totalLabel}
              </p>
            </div>
            <button
              type="button"
              onClick={onClose}
              class="h-6 w-6 inline-flex items-center justify-center rounded text-muted-foreground hover:text-foreground hover:bg-accent/60 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              aria-label="Close"
            >
              <svg class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
                <path d="M6 6l12 12M6 18L18 6" />
              </svg>
            </button>
          </div>
        </div>

        <ul class="flex-1 overflow-y-auto divide-y divide-border/60">
          {sessions.length === 0 && (
            <li class="px-4 py-6 text-center text-[12px] text-muted-foreground">
              No sessions recorded.
            </li>
          )}
          {sessions.map((s, i) => {
            const start = formatClock(s.signInAt);
            const end = s.signOutAt ? formatClock(s.signOutAt) : "active";
            const dur = formatHM(sessionMinutes(s));
            const isRunning = !s.signOutAt;
            return (
              <li key={i} class="px-4 py-2.5 flex items-center gap-3">
                <span
                  class={cn(
                    "inline-flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full text-[10px] font-semibold tabular-nums",
                    isRunning
                      ? "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300"
                      : "bg-muted text-muted-foreground",
                  )}
                  aria-hidden="true"
                >
                  {i + 1}
                </span>
                <div class="min-w-0 flex-1">
                  <p class="text-[12px] font-medium text-foreground leading-tight tabular-nums">
                    <span>{start}</span>
                    <span class="text-muted-foreground/50 mx-1.5">→</span>
                    <span class={isRunning ? "text-emerald-600 dark:text-emerald-400 font-semibold" : ""}>
                      {end}
                    </span>
                  </p>
                  {s.description && (
                    <p class="text-[10.5px] text-muted-foreground mt-0.5 truncate" title={s.description}>
                      {s.description}
                    </p>
                  )}
                </div>
                <span class="text-[12px] font-bold font-mono tabular-nums text-foreground/85 flex-shrink-0">
                  {dur}
                </span>
              </li>
            );
          })}
        </ul>

        <p class="px-4 py-1.5 text-[10px] text-muted-foreground/75 bg-muted/30 border-t border-border">
          Press <kbd class="font-mono px-1 py-px rounded bg-background border border-border text-[9.5px]">Esc</kbd> to close
        </p>
      </div>
    </div>
  );
}

function sessionMinutes(s: AttendanceSession): number {
  const start = Date.parse(s.signInAt);
  const end = s.signOutAt ? Date.parse(s.signOutAt) : Date.now();
  if (Number.isNaN(start) || Number.isNaN(end)) return 0;
  return Math.max(0, Math.floor((end - start) / 60000));
}

function formatClock(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false });
}

function formatHM(mins: number): string {
  const h = Math.floor(mins / 60);
  const m = mins % 60;
  return h > 0 ? `${h}h ${m}m` : `${m}m`;
}
