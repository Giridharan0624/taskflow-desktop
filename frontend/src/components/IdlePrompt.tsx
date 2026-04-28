import { useEffect } from "preact/hooks";
import { Button } from "./ui/Button";
import { cn } from "../lib/cn";

interface IdlePromptProps {
  /** Seconds the user has been idle. Drives the headline copy. */
  idleSeconds: number;
  onKeepTracking: () => void;
  onStopTimer: () => void;
}

/**
 * Idle-warning sheet.
 *
 * Surfaced once the activity monitor reports >5 min of no input
 * while a timer is running. Industry-standard pattern (Toggl,
 * Clockify, Hubstaff) for nudging the user before they accidentally
 * log a 4-hour lunch break against a billable task.
 *
 * Two actions:
 *   · Keep tracking — dismisses; the prompt will not re-appear for
 *     the same idle window (frontend gates against the dismiss
 *     timestamp). If the user goes idle AGAIN later, it'll reappear.
 *   · Stop timer    — calls SignOut so the bucket closes at "now"
 *     and the user can pick something else.
 *
 * "Discard idle time" (rolling back signInAt by N seconds) is a
 * future enhancement — would need a backend endpoint that mutates
 * the running session's signInAt without creating a new record.
 */
export function IdlePrompt({ idleSeconds, onKeepTracking, onStopTimer }: IdlePromptProps) {
  // Esc dismisses (interpreted as "keep tracking" — same as the
  // explicit primary CTA so accidental Esc never stops a timer).
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onKeepTracking();
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onKeepTracking]);

  const mins = Math.floor(idleSeconds / 60);
  const headline =
    mins < 60
      ? `${mins} minute${mins === 1 ? "" : "s"}`
      : `${Math.floor(mins / 60)}h ${mins % 60}m`;

  return (
    <div
      class="fixed inset-0 z-40 flex items-center justify-center px-4"
      role="dialog"
      aria-modal="true"
      aria-label="Idle warning"
    >
      <div
        class="absolute inset-0 bg-foreground/35 supports-[backdrop-filter]:bg-foreground/25 supports-[backdrop-filter]:backdrop-blur-[1.5px]"
        aria-hidden="true"
      />

      <div
        class={cn(
          "relative w-full max-w-xs",
          "rounded-lg border border-border bg-popover text-popover-foreground shadow-xl overflow-hidden",
          "animate-in fade-in zoom-in-95",
        )}
      >
        <div class="px-4 pt-4 pb-3 flex items-start gap-3">
          <span class="inline-flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-full bg-amber-500/15 text-amber-600 dark:text-amber-400">
            <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <circle cx="12" cy="12" r="10" />
              <path d="M12 6v6l4 2" />
            </svg>
          </span>
          <div class="min-w-0 flex-1">
            <h3 class="text-[14px] font-semibold text-foreground leading-tight">
              Still working?
            </h3>
            <p class="text-[11.5px] text-muted-foreground leading-snug mt-1">
              No activity detected for{" "}
              <span class="font-semibold text-foreground tabular-nums">{headline}</span>.
              Keep tracking, or stop the timer?
            </p>
          </div>
        </div>

        <div class="flex gap-1.5 px-3 pb-3 pt-1">
          <Button
            variant="outline"
            class="flex-1 h-8 text-[12px] font-medium"
            onClick={onStopTimer}
          >
            Stop timer
          </Button>
          <Button
            class="flex-1 h-8 text-[12px] font-semibold"
            onClick={onKeepTracking}
            autofocus
          >
            Keep tracking
          </Button>
        </div>
      </div>
    </div>
  );
}
