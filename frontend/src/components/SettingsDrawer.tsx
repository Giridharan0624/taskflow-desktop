import { useEffect, useState } from "preact/hooks";
import { Button } from "./ui/Button";
import { useSettings, clearAllLocalSettings, type ThemeChoice, type NotifPolicy } from "../lib/settings";
import { cn } from "../lib/cn";

interface SettingsDrawerProps {
  open: boolean;
  onClose: () => void;
}

/**
 * In-app preferences. Replaces the "go to the web dashboard" tax for
 * everyday tweaks (daily goal, idle thresholds, theme, autostart).
 *
 * Layout: full-height side drawer that slides in from the right.
 * Form sections are sized for the 450px-wide window. All settings
 * persist via the shared `useSettings` store; OS-level concerns
 * (auto-start, clear cache) call Wails bindings.
 */
export function SettingsDrawer({ open, onClose }: SettingsDrawerProps) {
  const [settings, update] = useSettings();
  const [autostartLoading, setAutostartLoading] = useState(false);
  const [clearing, setClearing] = useState(false);
  const [clearedAt, setClearedAt] = useState<number | null>(null);

  // Esc closes — same convention as the other overlays.
  useEffect(() => {
    if (!open) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  // Sync the persisted autoStartOnLogin flag with the OS-level state.
  // The Go binding writes the actual registry / LaunchAgent /
  // .desktop entry; we just call it whenever the toggle flips.
  async function setAutoStart(enabled: boolean) {
    setAutostartLoading(true);
    try {
      await window.go.main.App.SetAutoStart(enabled);
      update({ autoStartOnLogin: enabled });
    } catch {
      // Revert on failure — the OS write didn't take.
      // The toggle visually returns to its previous state.
    } finally {
      setAutostartLoading(false);
    }
  }

  async function clearCache() {
    if (clearing) return;
    if (
      !window.confirm(
        "Clear local cache?\n\n" +
          "This removes:\n" +
          "  • Queued heartbeats and screenshots not yet sent\n" +
          "  • Cached task list\n" +
          "  • Recent description history\n" +
          "  • Window-size preference\n\n" +
          "You will stay signed in. The cache rebuilds as you use the app.",
      )
    ) {
      return;
    }
    setClearing(true);
    try {
      await window.go.main.App.ClearLocalCache();
      clearAllLocalSettings();
      setClearedAt(Date.now());
      setTimeout(() => setClearedAt(null), 4000);
    } finally {
      setClearing(false);
    }
  }

  if (!open) return null;

  return (
    <div
      class="fixed inset-0 z-40 flex justify-end"
      role="dialog"
      aria-modal="true"
      aria-label="Settings"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        class="absolute inset-0 bg-foreground/30 supports-[backdrop-filter]:bg-foreground/20 supports-[backdrop-filter]:backdrop-blur-[1.5px]"
        aria-hidden="true"
      />

      <aside
        class={cn(
          "relative h-full w-full max-w-[360px] flex flex-col",
          "bg-popover text-popover-foreground border-l border-border shadow-2xl",
          "animate-in slide-in-from-right-2 fade-in",
        )}
      >
        <header class="flex items-center justify-between px-4 py-3 border-b border-border">
          <div>
            <p class="text-[9.5px] font-semibold uppercase tracking-[0.10em] text-muted-foreground/85 leading-none">
              Preferences
            </p>
            <h2 class="text-[15px] font-semibold text-foreground leading-tight mt-1">
              Settings
            </h2>
          </div>
          <button
            type="button"
            onClick={onClose}
            class="h-7 w-7 inline-flex items-center justify-center rounded text-muted-foreground hover:text-foreground hover:bg-accent/60 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            aria-label="Close"
          >
            <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.2" stroke-linecap="round">
              <path d="M6 6l12 12M6 18L18 6" />
            </svg>
          </button>
        </header>

        <div class="flex-1 overflow-y-auto">
          {/* ─── Theme ─── */}
          <Section
            title="Appearance"
            description="Light, dark, or follow your operating system."
          >
            <ThreeWaySegment
              value={settings.theme}
              options={[
                { v: "light", label: "Light", icon: <SunIcon /> },
                { v: "dark", label: "Dark", icon: <MoonIcon /> },
                { v: "system", label: "System", icon: <SystemIcon /> },
              ]}
              onChange={(v) => update({ theme: v as ThemeChoice })}
            />
          </Section>

          {/* ─── Daily goal ─── */}
          <Section
            title="Daily goal"
            description="Drives the progress ring around your avatar."
          >
            <NumberSlider
              value={settings.dailyGoalHours}
              min={1}
              max={16}
              step={0.5}
              suffix="h"
              onChange={(v) => update({ dailyGoalHours: v })}
            />
          </Section>

          {/* ─── Idle prompt threshold ─── */}
          <Section
            title="Idle handling"
            description="Show the “Still working?” prompt after this much inactivity. The timer auto-stops at 15 min regardless."
          >
            <NumberSlider
              label="Prompt after"
              value={Math.round(settings.idlePromptSeconds / 60)}
              min={1}
              // Cap below the fixed 15-min auto-stop so the prompt
              // always has a chance to fire before the auto-stop —
              // otherwise the user would never see the warning.
              max={14}
              step={1}
              suffix="min"
              onChange={(v) => update({ idlePromptSeconds: v * 60 })}
            />
            <p class="mt-2 text-[10.5px] text-muted-foreground/85">
              Auto-stop is fixed at <span class="font-semibold text-foreground/85">15 min</span> to catch forgotten timers.
            </p>
          </Section>

          {/* ─── Notifications ─── */}
          <Section
            title="Notifications"
            description="Tray balloons for events you might miss while the window is hidden — auto-stop, network drops, available updates."
          >
            <ThreeWaySegment
              value={settings.notifications}
              options={[
                { v: "all", label: "All" },
                { v: "errors-only", label: "Errors only" },
                { v: "off", label: "Off" },
              ]}
              onChange={(v) => update({ notifications: v as NotifPolicy })}
            />
            <ul class="mt-2 space-y-0.5 text-[10.5px] text-muted-foreground/85">
              <li>
                <span class="font-semibold text-foreground/85">All:</span> auto-stop, network drop/restore, update available
              </li>
              <li>
                <span class="font-semibold text-foreground/85">Errors only:</span> auto-stop and network drops
              </li>
              <li>
                <span class="font-semibold text-foreground/85">Off:</span> nothing (in-app banners still appear)
              </li>
            </ul>
          </Section>

          {/* ─── Auto-start ─── */}
          <Section
            title="Launch at startup"
            description="Open TaskFlow automatically when you sign in to your computer."
          >
            <Toggle
              checked={settings.autoStartOnLogin}
              onChange={setAutoStart}
              loading={autostartLoading}
              label={settings.autoStartOnLogin ? "Enabled" : "Disabled"}
            />
          </Section>

          {/* ─── Clear cache ─── */}
          <Section
            title="Local data"
            description="Clear queued events, cached tasks, and personal preferences. You stay signed in."
          >
            <Button
              type="button"
              variant="outline"
              class="h-8 text-[12px] font-medium"
              onClick={clearCache}
              disabled={clearing}
            >
              {clearing ? "Clearing…" : clearedAt ? "Cleared ✓" : "Clear local cache"}
            </Button>
          </Section>
        </div>

        <footer class="px-4 py-2 border-t border-border bg-muted/30">
          <p class="text-[10px] text-muted-foreground/85">
            Press{" "}
            <kbd class="font-mono px-1 py-px rounded bg-background border border-border text-[9.5px]">
              Esc
            </kbd>{" "}
            to close
          </p>
        </footer>
      </aside>
    </div>
  );
}

/* ─── primitives ─────────────────────────────────────────────── */

function Section({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: any;
}) {
  return (
    <section class="px-4 py-3.5 border-b border-border/60">
      <p class="text-[9.5px] font-semibold uppercase tracking-[0.10em] text-muted-foreground/85 leading-none">
        {title}
      </p>
      {description && (
        <p class="text-[11px] text-muted-foreground mt-1 mb-2.5 leading-snug">{description}</p>
      )}
      <div class={description ? "" : "mt-2.5"}>{children}</div>
    </section>
  );
}

function ThreeWaySegment({
  value,
  options,
  onChange,
}: {
  value: string;
  options: { v: string; label: string; icon?: any }[];
  onChange: (v: string) => void;
}) {
  return (
    <div class="inline-flex items-center rounded-md border border-input bg-background p-0.5 w-full">
      {options.map((o) => {
        const active = o.v === value;
        return (
          <button
            key={o.v}
            type="button"
            onClick={() => onChange(o.v)}
            class={cn(
              "flex-1 inline-flex items-center justify-center gap-1.5 h-7 px-2",
              "text-[11.5px] font-medium rounded transition-colors",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              active
                ? "bg-primary text-primary-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground hover:bg-accent/40",
            )}
          >
            {o.icon}
            {o.label}
          </button>
        );
      })}
    </div>
  );
}

function NumberSlider({
  label,
  value,
  min,
  max,
  step,
  suffix,
  onChange,
}: {
  label?: string;
  value: number;
  min: number;
  max: number;
  step: number;
  suffix: string;
  onChange: (v: number) => void;
}) {
  return (
    <div class="space-y-1.5">
      {label && (
        <div class="flex items-baseline justify-between text-[11.5px]">
          <span class="text-muted-foreground">{label}</span>
          <span class="font-semibold text-foreground tabular-nums">
            {value}
            <span class="text-muted-foreground/70 ml-0.5">{suffix}</span>
          </span>
        </div>
      )}
      {!label && (
        <div class="flex justify-end text-[11.5px]">
          <span class="font-semibold text-foreground tabular-nums">
            {value}
            <span class="text-muted-foreground/70 ml-0.5">{suffix}</span>
          </span>
        </div>
      )}
      <input
        type="range"
        min={min}
        max={max}
        step={step}
        value={value}
        onInput={(e) => onChange(Number((e.target as HTMLInputElement).value))}
        class={cn(
          "w-full h-1.5 appearance-none rounded-full bg-muted cursor-pointer",
          "accent-primary",
          "[&::-webkit-slider-thumb]:appearance-none",
          "[&::-webkit-slider-thumb]:h-3.5 [&::-webkit-slider-thumb]:w-3.5",
          "[&::-webkit-slider-thumb]:rounded-full [&::-webkit-slider-thumb]:bg-primary",
          "[&::-webkit-slider-thumb]:shadow [&::-webkit-slider-thumb]:cursor-grab",
          "[&::-webkit-slider-thumb]:active:cursor-grabbing",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        )}
      />
    </div>
  );
}

function Toggle({
  checked,
  onChange,
  loading,
  label,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  loading?: boolean;
  label?: string;
}) {
  return (
    <div class="flex items-center justify-between">
      <span class="text-[12px] text-foreground">{label}</span>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        onClick={() => onChange(!checked)}
        disabled={loading}
        class={cn(
          "relative inline-flex h-5 w-9 items-center rounded-full transition-colors",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:ring-offset-background",
          "disabled:cursor-wait disabled:opacity-70",
          checked ? "bg-primary" : "bg-muted",
        )}
      >
        <span
          class={cn(
            "inline-block h-4 w-4 transform rounded-full bg-background shadow transition-transform",
            checked ? "translate-x-[18px]" : "translate-x-[2px]",
          )}
        />
      </button>
    </div>
  );
}

/* ─── icons ─── */

function SunIcon() {
  return (
    <svg class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.2" aria-hidden="true">
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41" stroke-linecap="round" />
    </svg>
  );
}
function MoonIcon() {
  return (
    <svg class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.2" aria-hidden="true">
      <path d="M21 12.79A9 9 0 1111.21 3 7 7 0 0021 12.79z" />
    </svg>
  );
}
function SystemIcon() {
  return (
    <svg class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.2" aria-hidden="true">
      <rect x="2" y="4" width="20" height="14" rx="2" />
      <path d="M8 21h8M12 18v3" stroke-linecap="round" />
    </svg>
  );
}
