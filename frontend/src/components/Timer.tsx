import { useState, useEffect, useRef } from "preact/hooks";

interface TimerProps {
  startTime: string; // ISO timestamp
  class?: string;
}

/**
 * LiveTimer — displays elapsed time since startTime, ticking every second.
 * Mirrors the web app's LiveTimer component behavior exactly.
 */
export function Timer({ startTime, class: className }: TimerProps) {
  const [elapsed, setElapsed] = useState(0);
  const intervalRef = useRef<number | null>(null);

  useEffect(() => {
    const start = new Date(startTime).getTime();
    // If startTime is malformed (e.g. a backend field omitted / not yet
    // populated), Date.parse returns NaN and we would render "NaN:NaN:NaN".
    // Skip the interval entirely in that case and let the display fallback
    // take over. See M-FE-2.
    if (!Number.isFinite(start)) {
      setElapsed(NaN);
      return;
    }

    function tick() {
      const now = Date.now();
      setElapsed(Math.floor((now - start) / 1000));
    }

    tick(); // Immediate first tick
    intervalRef.current = window.setInterval(tick, 1000);

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
      }
    };
  }, [startTime]);

  const display = Number.isFinite(elapsed) && elapsed >= 0
    ? [
        String(Math.floor(elapsed / 3600)).padStart(2, "0"),
        String(Math.floor((elapsed % 3600) / 60)).padStart(2, "0"),
        String(elapsed % 60).padStart(2, "0"),
      ].join(":")
    : "--:--:--";

  return <span class={className || "timer-display"}>{display}</span>;
}

/**
 * Formats decimal hours to human-readable string (e.g., 2.5417 → "2h 32m 30s").
 * Always includes seconds for precision, matching the web app's format.
 */
export function formatDuration(decimalHours: number): string {
  if (decimalHours <= 0) return "0s";

  const totalSeconds = Math.round(decimalHours * 3600);
  const h = Math.floor(totalSeconds / 3600);
  const m = Math.floor((totalSeconds % 3600) / 60);
  const s = totalSeconds % 60;

  const parts: string[] = [];
  if (h > 0) parts.push(`${h}h`);
  if (m > 0) parts.push(`${m}m`);
  if (s > 0) parts.push(`${s}s`);

  return parts.join(" ") || "0s";
}
