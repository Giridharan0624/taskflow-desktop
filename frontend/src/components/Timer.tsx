import { useState, useEffect, useRef } from "preact/hooks";
import { serverNow } from "../lib/serverClock";

interface TimerProps {
  startTime: string; // ISO timestamp
  class?: string;
}

/**
 * LiveTimer — displays elapsed time since startTime, ticking every second.
 * Ticks against serverNow() (= Date.now() + offset) so two devices viewing
 * the same session agree on elapsed time even when their OS clocks drift.
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
      setElapsed(Math.floor((serverNow() - start) / 1000));
    }

    tick(); // Immediate first tick
    intervalRef.current = window.setInterval(tick, 1000);

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
      }
    };
  }, [startTime]);

  if (!(Number.isFinite(elapsed) && elapsed >= 0)) {
    return <span class={className || "timer-display"}>--:--:--</span>;
  }

  // Per-digit render so each digit can animate independently when
  // it changes. The seconds-ones digit ticks every second; the
  // tens, then minutes-ones, etc. tick at decreasing rates. Using
  // `key={digit}` on each <Digit /> forces Preact to remount that
  // node on change, which retriggers the digit-tick keyframe. P2-17.
  const hh = String(Math.floor(elapsed / 3600)).padStart(2, "0");
  const mm = String(Math.floor((elapsed % 3600) / 60)).padStart(2, "0");
  const ss = String(elapsed % 60).padStart(2, "0");

  return (
    <span class={className || "timer-display"} aria-label={`${hh}:${mm}:${ss} elapsed`}>
      <Digit ch={hh[0]} />
      <Digit ch={hh[1]} />
      <span class="opacity-60">:</span>
      <Digit ch={mm[0]} />
      <Digit ch={mm[1]} />
      <span class="opacity-60">:</span>
      <Digit ch={ss[0]} />
      <Digit ch={ss[1]} />
    </span>
  );
}

// One animated digit. Re-keyed by its own char so a value change
// remounts the span and replays the digit-tick keyframe. The
// inline-block + overflow-hidden wrapper keeps adjacent digits from
// shifting horizontally during the slide-in.
function Digit({ ch }: { ch: string }) {
  return (
    <span class="inline-block overflow-hidden align-baseline">
      <span key={ch} class="inline-block digit-tick">
        {ch}
      </span>
    </span>
  );
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
