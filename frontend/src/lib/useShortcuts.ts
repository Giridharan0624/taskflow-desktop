import { useEffect } from "preact/hooks";

/**
 * Lightweight global-keyboard-shortcut hook.
 *
 * Each entry maps a chord to a handler. The chord syntax is:
 *
 *   "Mod+Enter"   — Ctrl on Windows/Linux, Cmd on macOS
 *   "Mod+/"       — same, with a literal slash
 *   "Mod+Shift+R" — multi-modifier
 *   "Escape"      — bare key, no modifiers
 *   "?"           — bare shifted character (Shift+/ on US layouts);
 *                   matched by `event.key` directly so layout works
 *
 * We swallow the default browser behaviour (preventDefault) on every
 * matched chord — most chords here override standard browser
 * shortcuts (Ctrl+R, Ctrl+L, etc.) that don't make sense inside the
 * Wails webview.
 *
 * Bindings that target text inputs (e.g. focus the description) MUST
 * still fire when an input is focused; bindings that don't (e.g.
 * Start, Stop) are suppressed if the user is mid-typing in an input
 * or textarea so we don't hijack a normal Enter keystroke. The
 * `whenInInput` flag controls this.
 */
export interface Shortcut {
  chord: string;
  /** Run when the chord matches. */
  handler: (e: KeyboardEvent) => void;
  /** Default `false` — chord is suppressed when an input/textarea is
   *  focused. Set `true` for chords that should ALWAYS fire (focus
   *  shortcuts, the help overlay, etc.). */
  whenInInput?: boolean;
  /** Optional human-readable label for the help overlay. */
  label?: string;
}

const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);

function chordMatches(chord: string, e: KeyboardEvent): boolean {
  const parts = chord.split("+").map((p) => p.trim());
  const expectKey = parts[parts.length - 1];

  const wantMod = parts.some((p) => p === "Mod" || p === "Cmd" || p === "Ctrl");
  const wantShift = parts.includes("Shift");
  const wantAlt = parts.includes("Alt");
  const wantMeta = parts.some((p) => p === "Meta") && !wantMod;

  // Mod abstracts the OS-correct primary modifier.
  const modPressed = isMac ? e.metaKey : e.ctrlKey;
  if (wantMod !== modPressed) return false;
  if (wantShift !== e.shiftKey) return false;
  if (wantAlt !== e.altKey) return false;
  if (!wantMod) {
    // Only enforce Meta separately when the chord didn't already
    // consume it via "Mod" on macOS.
    if (wantMeta !== e.metaKey) return false;
  }

  // Key match — case-insensitive for letters, exact for everything
  // else (so "?" doesn't accidentally match "/").
  const k = e.key;
  if (expectKey.length === 1 && /[a-zA-Z]/.test(expectKey)) {
    return k.toLowerCase() === expectKey.toLowerCase();
  }
  return k === expectKey;
}

function isInInput(target: EventTarget | null): boolean {
  if (!target) return false;
  const el = target as HTMLElement;
  const tag = el.tagName?.toLowerCase();
  return (
    tag === "input" ||
    tag === "textarea" ||
    tag === "select" ||
    el.isContentEditable === true
  );
}

export function useShortcuts(shortcuts: Shortcut[]) {
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const inField = isInInput(e.target);
      for (const s of shortcuts) {
        if (!chordMatches(s.chord, e)) continue;
        if (inField && !s.whenInInput) continue;
        e.preventDefault();
        e.stopPropagation();
        s.handler(e);
        return;
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [shortcuts]);
}

/** Pretty-print a chord for the help overlay. "Mod" → "Ctrl" or "⌘". */
export function formatChord(chord: string): string {
  return chord
    .split("+")
    .map((p) => {
      if (p === "Mod") return isMac ? "⌘" : "Ctrl";
      if (p === "Shift") return isMac ? "⇧" : "Shift";
      if (p === "Alt") return isMac ? "⌥" : "Alt";
      if (p === "Enter") return "Enter";
      if (p === "Escape") return "Esc";
      return p;
    })
    .join(isMac ? "" : "+");
}
