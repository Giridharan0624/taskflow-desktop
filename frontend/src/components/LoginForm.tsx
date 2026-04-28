import { useEffect, useState } from "preact/hooks"
import type { User, LoginResult } from "../app"
import { useTheme } from "../lib/useTheme"
import { TaskFlowLogo } from "./Logo"
import { friendlyError } from "../lib/errors"
import { Button } from "./ui/Button"
import { Input } from "./ui/Input"
import { Label } from "./ui/Label"
import { cn } from "../lib/cn"

interface LoginFormProps {
  onSuccess: (user: User) => void
}

/**
 * LoginForm — refined sign-in surface.
 *
 * Stays inside the existing theme tokens (Lexend body, JetBrains
 * Mono for serial-numbers, shadcn HSL palette, indigo primary). The
 * polish comes from composition, not new colors:
 *
 *   - Asymmetric brand bar at top: logo + wordmark + monospace
 *     version chip (mimics a software build-stamp).
 *   - "ACCESS · 01 / 02" eyebrow above the form treats the auth
 *     flow as a numbered step in a pipeline — confidence cue.
 *   - Inputs grow an accent underline on focus (CSS-only, single
 *     pseudo-element). Subtle differentiation from stock shadcn.
 *   - Faint diagonal pinstripe in the background gives the surface
 *     texture without changing the palette.
 *   - Orchestrated mount stagger: brand bar (0ms) → eyebrow (80ms)
 *     → card (160ms) → fields cascade. One delightful page-load
 *     instead of scattered hover micro-interactions.
 *   - Footer carries an MMXXVI roman year + build version stamp,
 *     readable as "this is a real piece of software, signed off".
 */
export function LoginForm({ onSuccess }: LoginFormProps) {
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [newPassword, setNewPassword] = useState("")
  const [error, setError] = useState("")
  const [loading, setLoading] = useState(false)
  // The Cognito challenge session lives entirely on the Go side; the
  // frontend only tracks whether a challenge is pending.
  const [challengePending, setChallengePending] = useState(false)
  const { isDark, toggle } = useTheme()
  // App version — surfaced as a "build stamp" next to the wordmark.
  // Best-effort: missing binding falls through to no chip rather
  // than blocking the form's mount.
  const [version, setVersion] = useState<string>("")
  useEffect(() => {
    window.go.main.App.GetAppVersion().then(setVersion).catch(() => {})
  }, [])

  async function handleLogin(e: Event) {
    e.preventDefault()
    setError("")
    setLoading(true)
    try {
      const result: LoginResult = await window.go.main.App.Login(email, password)
      // Clear the password out of state immediately after the Cognito
      // call returns — both on the happy path AND on the new-password
      // challenge branch. Previous behaviour kept the plaintext alive
      // for the entire MFA flow; a mid-challenge unmount would retain
      // it until GC. See H-FE-2.
      setPassword("")
      if (result.requiresNewPassword) {
        setChallengePending(true)
        setLoading(false)
        return
      }
      onSuccess(await window.go.main.App.GetCurrentUser())
    } catch (err: any) {
      setError(friendlyError(err))
    } finally {
      setLoading(false)
    }
  }

  async function handleNewPassword(e: Event) {
    e.preventDefault()
    setError("")
    setLoading(true)
    try {
      await window.go.main.App.SetNewPassword(newPassword)
      // Clear both password fields after the challenge completes. H-FE-2.
      setPassword("")
      setNewPassword("")
      onSuccess(await window.go.main.App.GetCurrentUser())
    } catch (err: any) {
      setError(friendlyError(err))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div class="relative flex flex-col h-full bg-background overflow-hidden">
      {/* ── Background atmosphere ───────────────────────────────
          Two layers of subtle texture inside the theme palette:
            1. A primary-tinted gradient haloed at the top
            2. A faint diagonal pinstripe behind everything
          Both stay below 6% opacity so they read as "considered
          surface" rather than decoration.
      ─────────────────────────────────────────────────────────── */}
      <div
        class="pointer-events-none absolute inset-0 z-0 opacity-[0.55]"
        aria-hidden="true"
        style={{
          backgroundImage:
            "repeating-linear-gradient(135deg, hsl(var(--foreground) / 0.02) 0 1px, transparent 1px 9px)",
        }}
      />
      <div
        class="pointer-events-none absolute -top-32 left-1/2 -translate-x-1/2 h-[420px] w-[520px] rounded-full blur-3xl bg-primary/[0.07]"
        aria-hidden="true"
      />
      <div
        class="pointer-events-none absolute inset-x-0 top-0 h-32 bg-gradient-to-b from-background/0 via-background/0 to-background"
        aria-hidden="true"
      />

      {/* ── Top bar ─────────────────────────────────────────────
          Brand bar on the left (logo + wordmark + version chip),
          theme toggle on the right. Asymmetric — most desktop
          login screens center everything; this one anchors the
          identity to the corner where eyes track first.
      ─────────────────────────────────────────────────────────── */}
      <header class="relative z-10 flex items-center justify-between gap-3 px-4 py-3 login-anim" style={{ animationDelay: "0ms" }}>
        <div class="flex items-center gap-2.5">
          <TaskFlowLogo size={26} />
          <div class="flex items-baseline gap-2">
            <h1 class="text-[14px] font-extrabold tracking-[-0.015em] text-foreground leading-none">
              Task<span class="text-primary">Flow</span>
            </h1>
            {version && (
              <span
                class="font-mono text-[9.5px] font-medium px-1.5 py-0.5 rounded border border-border bg-muted/60 text-muted-foreground tabular-nums tracking-[0.02em]"
                title={`Build ${version}`}
              >
                v{version.replace(/^v/, "")}
              </span>
            )}
          </div>
        </div>
        <Button
          variant="ghost"
          size="icon"
          class="h-7 w-7 text-muted-foreground hover:text-foreground hover:bg-accent/60"
          onClick={toggle}
          aria-label={isDark ? "Switch to light mode" : "Switch to dark mode"}
          title={isDark ? "Light mode" : "Dark mode"}
        >
          {isDark ? <SunIcon /> : <MoonIcon />}
        </Button>
      </header>

      {/* ── Main form area ────────────────────────────────────── */}
      <div class="relative z-10 flex-1 flex flex-col items-center justify-center px-6 pb-3">
        <div class="w-full max-w-[360px]">

          {/* Heading. Display-grade weight contrast: the verb-led
              first line is bold, the second line is light + muted.
              No "ACCESS · 01 / 02" eyebrow — it implied a numbered
              pipeline every user walks through, which is wrong:
              step 02 only appears for first-time-login users hitting
              the NEW_PASSWORD_REQUIRED challenge. */}
          <div
            class="login-anim mb-5"
            style={{ animationDelay: "80ms" }}
          >
            <h2 class="text-[22px] font-extrabold tracking-[-0.02em] text-foreground leading-[1.05]">
              {challengePending ? "Set a new password" : "Sign in to TaskFlow"}
            </h2>
            <p class="mt-1.5 text-[12px] text-muted-foreground leading-snug">
              {challengePending
                ? "First-time login — choose a password to continue."
                : "Track focused time, surface daily progress, ship more."}
            </p>
          </div>

          {/* Card-less form — the page itself IS the surface. The
              previous shadcn Card felt like a generic SaaS modal;
              dropping it lets the form sit on the textured page
              and feel native to this app. */}
          <div
            class="login-anim"
            style={{ animationDelay: "200ms" }}
          >
            {challengePending ? (
              <form onSubmit={handleNewPassword} class="space-y-3.5">
                <FieldRow delay={260}>
                  <Label htmlFor="newpw" class="text-[10.5px] font-semibold uppercase tracking-[0.1em] text-muted-foreground">
                    New password
                  </Label>
                  <FocusInput
                    id="newpw"
                    type="password"
                    placeholder="At least 8 characters"
                    value={newPassword}
                    onInput={(e: Event) => setNewPassword((e.target as HTMLInputElement).value)}
                    required
                    minLength={8}
                    autoFocus
                  />
                </FieldRow>

                {error && <ErrorBox msg={error} />}

                <SubmitButton loading={loading} label="Set password & continue" delay={340} />
              </form>
            ) : (
              <form onSubmit={handleLogin} class="space-y-3.5">
                <FieldRow delay={260}>
                  <Label htmlFor="identifier" class="text-[10.5px] font-semibold uppercase tracking-[0.1em] text-muted-foreground">
                    Email or Employee ID
                  </Label>
                  <FocusInput
                    id="identifier"
                    type="text"
                    placeholder="Email or employee ID"
                    value={email}
                    onInput={(e: Event) => setEmail((e.target as HTMLInputElement).value)}
                    required
                    autoFocus
                  />
                </FieldRow>

                <FieldRow delay={320}>
                  <Label htmlFor="password" class="text-[10.5px] font-semibold uppercase tracking-[0.1em] text-muted-foreground">
                    Password
                  </Label>
                  <FocusInput
                    id="password"
                    type="password"
                    placeholder="Enter your password"
                    value={password}
                    onInput={(e: Event) => setPassword((e.target as HTMLInputElement).value)}
                    required
                  />
                </FieldRow>

                {error && <ErrorBox msg={error} />}

                <SubmitButton loading={loading} label="Sign in" delay={380} />
              </form>
            )}
          </div>

          {/* Trust strip below the form. Soft ambient detail —
              users hovering before they type their password get a
              quiet reminder that the auth path is real. */}
          <div
            class="login-anim mt-5 pt-3 border-t border-border/50 flex items-center gap-2 text-[10px] text-muted-foreground/85"
            style={{ animationDelay: "440ms" }}
          >
            <svg class="h-3 w-3 text-primary/70" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <rect x="3" y="11" width="18" height="11" rx="2" />
              <path d="M7 11V7a5 5 0 0 1 10 0v4" />
            </svg>
            <span>
              SRP authentication via Cognito · TLS 1.3
            </span>
          </div>
        </div>
      </div>

      {/* ── Footer ──────────────────────────────────────────────
          Editorial-style stamp: organization mark + roman year +
          build channel. Reads as "signed off by a real team",
          which is the entire purpose of including it. */}
      <footer
        class="relative z-10 px-4 py-2.5 flex items-center justify-between text-[9.5px] text-muted-foreground/85 font-medium tracking-[0.06em] uppercase login-anim"
        style={{ animationDelay: "500ms" }}
      >
        <span class="font-mono tabular-nums">NeuroStack · MMXXVI</span>
        <span class="font-mono tabular-nums text-muted-foreground/65">
          {version ? `Build ${version.replace(/^v/, "").split(".").slice(0, 3).join(".")}` : "Build —"}
        </span>
      </footer>

      {/* Local stylesheet — keeps the focus-underline + mount
          stagger isolated to this component without polluting the
          global tailwind keyframes. */}
      <style>{`
        @keyframes login-rise {
          from { opacity: 0; transform: translateY(6px); }
          to   { opacity: 1; transform: translateY(0); }
        }
        .login-anim {
          opacity: 0;
          animation: login-rise 0.5s cubic-bezier(.2,.7,.2,1) forwards;
        }

        /* Focus underline — a 2px primary bar that grows from
           left to full width when the wrapped input is focused.
           Sits inside the FocusInput wrapper element via
           ::after; the wrapper has 'group' class so peer
           selectors hit the input. */
        .focus-input { position: relative; }
        .focus-input::after {
          content: "";
          position: absolute;
          left: 0; right: auto; bottom: 0;
          height: 2px;
          width: 0;
          background: hsl(var(--primary));
          transition: width 0.28s cubic-bezier(.2,.7,.2,1);
          pointer-events: none;
          border-bottom-left-radius: var(--radius);
          border-bottom-right-radius: var(--radius);
        }
        .focus-input:focus-within::after { width: 100%; }
      `}</style>
    </div>
  )
}

// ─── Composition helpers ──────────────────────────────────────

function FieldRow({
  delay,
  children,
}: {
  delay: number
  children: any
}) {
  return (
    <div
      class="space-y-1 login-anim"
      style={{ animationDelay: `${delay}ms` }}
    >
      {children}
    </div>
  )
}

// FocusInput wraps the shadcn Input in a span that owns the focus
// underline. The ::after pseudo lives on the wrapper because the
// real <input> can't host a pseudo-element. Forwarding all standard
// input props through.
function FocusInput(props: any) {
  return (
    <span class="focus-input block">
      <Input {...props} class="bg-background/60 backdrop-blur-[1px]" />
    </span>
  )
}

function SubmitButton({
  loading,
  label,
  delay,
}: {
  loading: boolean
  label: string
  delay: number
}) {
  return (
    <div
      class="login-anim pt-1"
      style={{ animationDelay: `${delay}ms` }}
    >
      <Button
        type="submit"
        class={cn(
          "w-full h-10 font-semibold gap-2 group",
          "shadow-sm hover:shadow",
          "tracking-[-0.005em]",
        )}
        disabled={loading}
      >
        {loading ? (
          <span class="opacity-90">{label.endsWith("…") ? label : `${label}…`.replace("Sign in…", "Signing in…").replace("Set password & continue…", "Setting password…")}</span>
        ) : (
          <>
            <span>{label}</span>
            <svg
              class="h-3 w-3 transition-transform group-hover:translate-x-0.5"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2.4"
              stroke-linecap="round"
              stroke-linejoin="round"
              aria-hidden="true"
            >
              <path d="M5 12h14M13 5l7 7-7 7" />
            </svg>
          </>
        )}
      </Button>
    </div>
  )
}

function ErrorBox({ msg }: { msg: string }) {
  return (
    <div
      role="alert"
      class={cn(
        "flex items-start gap-2 rounded-md border border-destructive/25 bg-destructive/[0.07] px-2.5 py-2",
        "text-[11.5px] leading-snug text-destructive",
        "animate-in fade-in slide-in-from-top-1",
      )}
    >
      <svg
        class="mt-px h-3.5 w-3.5 flex-shrink-0"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
        stroke-width="2"
        stroke-linecap="round"
        stroke-linejoin="round"
        aria-hidden="true"
      >
        <circle cx="12" cy="12" r="10" />
        <path d="M12 8v4M12 16h.01" />
      </svg>
      <span class="font-medium">{msg}</span>
    </div>
  )
}

function SunIcon() {
  return (
    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
      <circle cx="12" cy="12" r="5" />
      <path d="M12 1v2M12 21v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M1 12h2M21 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42" />
    </svg>
  )
}

function MoonIcon() {
  return (
    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
      <path d="M21 12.79A9 9 0 1111.21 3 7 7 0 0021 12.79z" />
    </svg>
  )
}
