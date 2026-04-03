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

  const hours = Math.floor(elapsed / 3600);
  const minutes = Math.floor((elapsed % 3600) / 60);
  const seconds = elapsed % 60;

  const display = [
    String(hours).padStart(2, "0"),
    String(minutes).padStart(2, "0"),
    String(seconds).padStart(2, "0"),
  ].join(":");

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
