import { useState, useEffect, useRef, useId } from "preact/hooks";
import type { User } from "../app";
import { cn } from "../lib/cn";

interface AvatarMenuProps {
  user: User;
  /** Hours logged today — drives the progress ring. */
  todayHours: number;
  /** Tenant-configured daily goal in hours. Falls back to 8 when zero
   *  or absent so the ring still has something to render against. */
  goalHours: number;
  /** OS-aware theme state. */
  isDark: boolean;
  onToggleTheme: () => void;
  onLogout: () => void;
  /** Open the in-app settings drawer. */
  onOpenSettings: () => void;
  /** Optional dashboard URL — when present, surfaces a "Web dashboard"
   *  menu item that opens externally. */
  dashboardURL?: string;
  /** Build-time version string injected by ldflags. Surfaced in the
   *  menu footer so users can reference it in support tickets. */
  version?: string;
}

/**
 * Header avatar with two jobs:
 *
 *   1. Progress ring around the avatar showing today vs daily goal
 *      — gives the user an at-a-glance sense of their day without
 *      having to read totals. Stops at 100% (no overflow ring) and
 *      goes emerald-green at 100% as a quiet reward signal.
 *   2. Click-to-open dropdown menu consolidating: theme toggle, web
 *      dashboard, version, sign out. Frees the header from carrying
 *      two separate buttons and matches the ergonomics every modern
 *      web/desktop app has trained the user to expect.
 */
export function AvatarMenu({
  user,
  todayHours,
  goalHours,
  isDark,
  onToggleTheme,
  onLogout,
  onOpenSettings,
  dashboardURL,
  version,
}: AvatarMenuProps) {
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLDivElement>(null);
  const ringId = useId(); // unique gradient id even with multiple instances
  const goal = goalHours > 0 ? goalHours : 8;
  const progress = Math.max(0, Math.min(1, todayHours / goal));
  const goalReached = progress >= 1;

  // Close on outside click + Escape.
  useEffect(() => {
    if (!open) return;
    function onDoc(e: MouseEvent) {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") setOpen(false);
    }
    document.addEventListener("mousedown", onDoc);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDoc);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  // SVG ring geometry — circumference for stroke-dasharray.
  const r = 17;
  const c = 2 * Math.PI * r;
  const dashOffset = c * (1 - progress);

  return (
    <div class="relative" ref={wrapRef}>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-label="Account menu"
        aria-haspopup="menu"
        aria-expanded={open}
        title={`${todayHours.toFixed(1)}h of ${goal}h goal`}
        class={cn(
          "relative flex items-center gap-2.5 rounded-full p-0.5 pr-2.5",
          "transition-colors hover:bg-accent/50",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:ring-offset-background",
        )}
      >
        {/* Avatar w/ progress ring */}
        <span class="relative inline-flex h-9 w-9 flex-shrink-0">
          {/* SVG ring — sits behind the avatar visually but lays
              out at the same size. The ring track is muted; the
              progress arc is primary (or emerald when goalReached).
              CSS transitions the dashOffset so changes animate. */}
          <svg
            class="absolute inset-0 -rotate-90"
            viewBox="0 0 40 40"
            aria-hidden="true"
          >
            <circle
              cx="20"
              cy="20"
              r={r}
              fill="none"
              stroke="hsl(var(--border))"
              stroke-width="2"
            />
            <circle
              cx="20"
              cy="20"
              r={r}
              fill="none"
              stroke={goalReached ? "rgb(16 185 129)" : `url(#${ringId})`}
              stroke-width="2"
              stroke-linecap="round"
              stroke-dasharray={c}
              stroke-dashoffset={dashOffset}
              style={{ transition: "stroke-dashoffset 0.6s cubic-bezier(.2,.7,.2,1)" }}
            />
            <defs>
              <linearGradient id={ringId} x1="0%" y1="0%" x2="100%" y2="100%">
                <stop offset="0%" stop-color="rgb(var(--color-primary, 99 102 241))" stop-opacity="0.6" />
                <stop offset="100%" stop-color="rgb(var(--color-primary, 99 102 241))" stop-opacity="1" />
              </linearGradient>
            </defs>
          </svg>

          {/* Avatar itself sits centered inside the ring with a tiny
              inset so the ring doesn't crash into the image. */}
          {user.avatarUrl ? (
            <img
              src={user.avatarUrl}
              alt=""
              class="absolute inset-[3px] h-[calc(100%-6px)] w-[calc(100%-6px)] rounded-full object-cover"
            />
          ) : (
            <span class="absolute inset-[3px] inline-flex items-center justify-center rounded-full bg-primary/10 text-[11px] font-bold text-primary tracking-tight">
              {user.name?.charAt(0).toUpperCase() || "?"}
            </span>
          )}
        </span>

        {/* Identity strip — same content as before, just lifted into
            the same button so the whole zone feels like one chip. */}
        <span class="flex flex-col items-start min-w-0">
          <span class="text-[13px] font-semibold leading-[1.15] text-foreground truncate max-w-[160px]">
            {user.name}
          </span>
          <span class="text-[10px] text-muted-foreground leading-tight tabular-nums tracking-[0.005em]">
            <span class="font-semibold text-foreground/80">{todayHours.toFixed(1)}h</span>
            <span class="text-muted-foreground/50 mx-1">/</span>
            <span>{goal}h goal</span>
          </span>
        </span>
      </button>

      {/* Dropdown menu — anchored to the LEFT of the trigger because
          the avatar lives in the top-left corner of a 450px-wide
          window. Anchoring `right-0` (right edge of trigger) would
          punch the menu off the left edge of the window. We also cap
          the width so even on a 450 px window the menu fits with a
          small breathing margin. */}
      {open && (
        <div
          role="menu"
          class={cn(
            "absolute left-0 top-full mt-1.5 z-40 w-[260px] max-w-[calc(100vw-24px)] origin-top-left",
            "rounded-lg border border-border bg-popover text-popover-foreground shadow-xl overflow-hidden",
            "animate-in fade-in slide-in-from-top-1",
          )}
        >
          {/* Identity header — repeats the user info inside the menu
              so once it's open the button itself is just a trigger
              (which can show running-state info without crowding). */}
          <div class="px-3 pt-2.5 pb-2 border-b border-border bg-muted/30">
            <p class="text-[12px] font-semibold text-foreground leading-tight truncate">{user.name}</p>
            <p class="text-[10px] text-muted-foreground leading-tight mt-0.5 truncate">
              {user.employeeId && (
                <span class="font-semibold text-primary tabular-nums">{user.employeeId}</span>
              )}
              {user.employeeId && <span class="text-muted-foreground/40 mx-1">·</span>}
              {user.email}
            </p>
          </div>

          {/* Today's progress — repeats the ring's data as exact
              numbers for users who want the precise figure. */}
          <div class="px-3 py-2 border-b border-border/60">
            <div class="flex items-baseline justify-between mb-1">
              <span class="text-[10px] font-semibold uppercase tracking-[0.10em] text-muted-foreground/85">
                Today
              </span>
              <span class="text-[11px] font-mono tabular-nums">
                <span class="font-bold text-foreground">{todayHours.toFixed(1)}h</span>
                <span class="text-muted-foreground/50 mx-0.5">/</span>
                <span class="text-muted-foreground">{goal}h</span>
              </span>
            </div>
            <div class="h-1 rounded-full bg-muted overflow-hidden">
              <div
                class={cn(
                  "h-full transition-all duration-500",
                  goalReached ? "bg-emerald-500" : "bg-primary",
                )}
                style={{ width: `${progress * 100}%` }}
              />
            </div>
          </div>

          <ul class="py-1">
            <MenuItem
              onClick={() => {
                onOpenSettings();
                setOpen(false);
              }}
              icon={<SettingsIcon />}
              label="Settings"
            />
            <MenuItem
              onClick={() => {
                onToggleTheme();
                setOpen(false);
              }}
              icon={isDark ? <SunIcon /> : <MoonIcon />}
              label={isDark ? "Switch to light mode" : "Switch to dark mode"}
            />
            {dashboardURL && (
              <li role="none">
                <a
                  role="menuitem"
                  // Deep-link to the /dashboard route rather than the
                  // marketing root — if the user is already signed
                  // in to the web app in their browser, they land on
                  // their dashboard immediately. If they aren't,
                  // Next.js middleware redirects them to /login,
                  // which is still better than dropping them on the
                  // public landing page they then have to click
                  // through. Trailing-slash safe.
                  href={dashboardURL.replace(/\/+$/, "") + "/dashboard"}
                  target="_blank"
                  onClick={() => setOpen(false)}
                  class={cn(
                    "flex items-center gap-2.5 px-3 py-2 text-[12px] text-foreground",
                    "hover:bg-accent hover:text-accent-foreground transition-colors",
                    "focus-visible:outline-none focus-visible:bg-accent",
                  )}
                >
                  <ExternalIcon />
                  <span class="flex-1">Open web dashboard</span>
                  <span class="opacity-60">↗</span>
                </a>
              </li>
            )}
            <li class="my-1 border-t border-border/60" role="separator" />
            <MenuItem
              onClick={() => {
                onLogout();
                setOpen(false);
              }}
              icon={<SignOutIcon />}
              label="Sign out"
              variant="destructive"
            />
          </ul>

          {version && (
            <div class="px-3 py-1.5 border-t border-border bg-muted/30">
              <p class="text-[9.5px] font-mono text-muted-foreground/85 tabular-nums tracking-[0.02em]">
                TaskFlow Desktop · v{version}
              </p>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function MenuItem({
  onClick,
  icon,
  label,
  variant = "default",
}: {
  onClick: () => void;
  icon: any;
  label: string;
  variant?: "default" | "destructive";
}) {
  return (
    <li role="none">
      <button
        type="button"
        role="menuitem"
        onClick={onClick}
        class={cn(
          "w-full flex items-center gap-2.5 px-3 py-2 text-[12px] text-left transition-colors",
          variant === "destructive"
            ? "text-destructive hover:bg-destructive/[0.08]"
            : "text-foreground hover:bg-accent hover:text-accent-foreground",
          "focus-visible:outline-none focus-visible:bg-accent",
        )}
      >
        {icon}
        <span class="flex-1">{label}</span>
      </button>
    </li>
  );
}

function SunIcon() {
  return (
    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
      <circle cx="12" cy="12" r="5" />
      <path d="M12 1v2M12 21v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M1 12h2M21 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42" />
    </svg>
  );
}
function MoonIcon() {
  return (
    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
      <path d="M21 12.79A9 9 0 1111.21 3 7 7 0 0021 12.79z" />
    </svg>
  );
}
function SettingsIcon() {
  return (
    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33h.09a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
    </svg>
  );
}

function ExternalIcon() {
  return (
    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
      <path d="M15 3h6v6M10 14L21 3" />
    </svg>
  );
}
function SignOutIcon() {
  return (
    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
      <path stroke-linecap="round" stroke-linejoin="round" d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1" />
    </svg>
  );
}
