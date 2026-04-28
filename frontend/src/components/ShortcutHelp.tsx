import { formatChord } from "../lib/useShortcuts";

export interface ShortcutEntry {
  chord: string;
  label: string;
}

interface ShortcutHelpProps {
  open: boolean;
  onClose: () => void;
  shortcuts: ShortcutEntry[];
}

/**
 * Modal-ish overlay listing the available keyboard shortcuts. Opened
 * via "?" (no modifier needed — it's a bare-Shift+slash chord that
 * the global handler treats as "always fire even in inputs"), closed
 * via Escape, click-outside, or the explicit ✕.
 *
 * Visual: centred card, soft backdrop blur, monospace chord chips on
 * the right, label on the left. Sticks to the existing theme tokens
 * — no new colors.
 */
export function ShortcutHelp({ open, onClose, shortcuts }: ShortcutHelpProps) {
  if (!open) return null;
  return (
    <div
      class="fixed inset-0 z-50 flex items-center justify-center px-4 animate-in fade-in"
      role="dialog"
      aria-modal="true"
      aria-label="Keyboard shortcuts"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      {/* Backdrop. backdrop-blur is conditional — Wails on Linux/X11
          can struggle with it; supports() lets the browser opt out. */}
      <div
        class="absolute inset-0 bg-foreground/40 backdrop-blur-[1.5px] supports-[backdrop-filter]:bg-foreground/30"
        aria-hidden="true"
      />

      {/* Card. Max-width capped well below the 800px window max so it
          sits comfortably regardless of resize. */}
      <div class="relative w-full max-w-xs rounded-lg border border-border bg-popover text-popover-foreground shadow-xl overflow-hidden">
        <div class="flex items-center justify-between px-3 py-2 border-b border-border">
          <p class="text-[10.5px] font-semibold uppercase tracking-[0.10em] text-muted-foreground/85">
            Keyboard Shortcuts
          </p>
          <button
            type="button"
            onClick={onClose}
            class="h-5 w-5 inline-flex items-center justify-center rounded text-muted-foreground hover:text-foreground hover:bg-accent/60 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            aria-label="Close"
          >
            <svg class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
              <path d="M6 6l12 12M6 18L18 6" />
            </svg>
          </button>
        </div>

        <ul class="divide-y divide-border/60">
          {shortcuts.map((s, i) => (
            <li
              key={i}
              class="flex items-center justify-between gap-3 px-3 py-2 text-[12px]"
            >
              <span class="text-foreground leading-tight">{s.label}</span>
              <kbd class="font-mono text-[10.5px] font-semibold tabular-nums bg-muted text-foreground/85 border border-border rounded px-1.5 py-0.5 shadow-[0_1px_0_rgba(15,17,35,0.05)]">
                {formatChord(s.chord)}
              </kbd>
            </li>
          ))}
        </ul>

        <p class="px-3 py-1.5 text-[10px] text-muted-foreground/75 bg-muted/30 border-t border-border">
          Press <kbd class="font-mono px-1 py-0.5 rounded bg-background border border-border text-[9.5px]">Esc</kbd> to close
        </p>
      </div>
    </div>
  );
}
