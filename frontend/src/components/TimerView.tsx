import { useState, useEffect, useMemo } from "preact/hooks";
import type {
  User,
  Attendance,
  AttendanceSession,
  StartTimerData,
  UpdateInfo,
  SessionInfo,
} from "../app";
import { Timer, formatDuration } from "./Timer";
import { TaskSelector } from "./TaskSelector";
import { TaskFlowLogo } from "./Logo";
import { ShortcutHelp } from "./ShortcutHelp";
import { AvatarMenu } from "./AvatarMenu";
import { SettingsDrawer } from "./SettingsDrawer";
import { SessionInspect } from "./SessionInspect";
import { IdlePrompt } from "./IdlePrompt";
import { useTheme } from "../lib/useTheme";
import { friendlyError } from "../lib/errors";
import { Button } from "./ui/Button";
import { Card } from "./ui/Card";
import { cn } from "../lib/cn";
import { recordServerTime, serverNow } from "../lib/serverClock";
import { useShortcuts } from "../lib/useShortcuts";
import { useSettings } from "../lib/settings";
import { colorForProject } from "../lib/projectColor";
import { notify } from "../lib/notify";

interface TimerViewProps {
  user: User;
  onLogout: () => void;
}

// Module-level variable to persist optimistic timestamp across re-renders and polling
let _optimisticSignInAt: string | null = null;

// localStorage key for the Wayland-limitation banner dismissal. Kept
// in module scope so multiple TimerView mounts (e.g. fast-refresh) all
// agree on whether the user already dismissed it this device.
const SESSION_BANNER_DISMISSED_KEY = "sessionBannerDismissed";

// Idle poll cadence. The prompt threshold is user-tunable from the
// settings drawer; the auto-stop threshold is INTENTIONALLY a fixed
// constant — auto-stop catches forgotten timers and is the safety
// floor of the time-tracking contract, so we don't let users lift
// it to "8 hours" and silently bill a forgotten lunch.
const IDLE_POLL_MS = 30_000;
const IDLE_AUTO_STOP_SECONDS = 15 * 60;

export function TimerView({ user, onLogout }: TimerViewProps) {
  const [settings] = useSettings();
  const [attendance, setAttendance] = useState<Attendance | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  // Dashboard URL is resolved from the Go config (ldflags-injected per
  // build variant) instead of a hard-coded prod URL. See M-FE-3.
  const [dashboardURL, setDashboardURL] = useState("");
  // sessionBanner shows a one-time banner when the OS display server
  // imposes tracking limits (primarily: GNOME Wayland on Ubuntu 24.04,
  // where the compositor hides per-app focus from non-privileged apps).
  // Dismissed state persists in localStorage.
  const [sessionBanner, setSessionBanner] = useState("");
  // tickCount drives per-second re-renders of the active timer display.
  // Readable (not the discarded `[, tick]` placeholder) so useMemo can
  // depend on it directly instead of on Date.now(). See H-FE-1.
  const [tickCount, setTick] = useState(0);
  const isActive = attendance?.status === "SIGNED_IN";
  const [helpOpen, setHelpOpen] = useState(false);
  // SessionInspect drawer — populated when the user right-clicks /
  // long-presses a TaskRow. We pass the FILTERED sessions matching
  // that grouped task so the user sees only the relevant entries.
  const [inspectingTaskKey, setInspectingTaskKey] = useState<string | null>(null);
  // Undo-after-Stop toast. When the user stops a timer, we capture
  // its task data here and surface a "Stopped — Undo" toast for 5 s.
  // Clicking Undo within the window calls SignIn with the captured
  // data, restarting the same task. The server records this as a
  // new session (we can't actually un-close a closed session
  // without a backend endpoint), but the perceived behavior is
  // "the timer kept going" which is what users want from undo.
  const [undoableStop, setUndoableStop] = useState<StartTimerData | null>(null);
  // Idle-prompt state:
  //   idleSecondsForPrompt — last sampled idle reading; drives the
  //     "X minutes" copy in the dialog.
  //   idleAck — when set, the user has dismissed the prompt for the
  //     CURRENT idle window. We re-prompt only after the user
  //     becomes active again (idle resets near 0) so they don't get
  //     hit with the same dialog every poll while continuing to be
  //     idle.
  const [idleSecondsForPrompt, setIdleSecondsForPrompt] = useState(0);
  const [idleAck, setIdleAck] = useState(false);
  const [idlePromptOpen, setIdlePromptOpen] = useState(false);
  // autoStoppedNotice surfaces a brief toast when the timer was
  // ended automatically due to long-idle. Distinguished from the
  // post-Stop "Undo" toast because there's no undo here — the user
  // wasn't even at the keyboard. Auto-clears after 8 s.
  const [autoStoppedNotice, setAutoStoppedNotice] = useState<{
    minutes: number;
    taskTitle: string;
  } | null>(null);

  useEffect(() => {
    if (!isActive) return;
    const i = setInterval(() => setTick((t) => t + 1), 1000);
    return () => clearInterval(i);
  }, [isActive]);

  // Idle poll — only ticks while a timer is active. The Go side
  // exposes idle seconds via App.GetIdleSeconds; we don't need
  // sub-second precision (the threshold is 5 minutes) so a 30 s
  // poll is plenty and doesn't burden IPC.
  useEffect(() => {
    if (!isActive) {
      setIdlePromptOpen(false);
      setIdleAck(false);
      setIdleSecondsForPrompt(0);
      return;
    }
    let cancelled = false;
    async function tick() {
      try {
        const idle = await window.go.main.App.GetIdleSeconds();
        if (cancelled) return;
        setIdleSecondsForPrompt(idle);

        // Stage 2: auto-stop after IDLE_AUTO_STOP_SECONDS, regardless
        // of whether the prompt was acknowledged. "Keep tracking"
        // suppresses the dialog's re-appearance for the current idle
        // window, but it does NOT exempt the user from auto-stop —
        // the whole point is to catch forgotten timers.
        if (idle >= IDLE_AUTO_STOP_SECONDS) {
          const cur = attendance?.currentTask;
          const taskTitle = cur?.taskTitle || "Timer";
          setIdlePromptOpen(false);
          setIdleAck(false);
          setIdleSecondsForPrompt(0);
          setAutoStoppedNotice({
            minutes: Math.floor(idle / 60),
            taskTitle,
          });
          // Tray balloon for users who minimized the app — without
          // this they'd come back hours later wondering why their
          // timer stopped. Classed as "error" tier so the
          // Errors-only policy still fires it: this is a state the
          // user MUST know about. Off policy still suppresses.
          notify(
            "error",
            "TaskFlow timer auto-stopped",
            `${taskTitle} was idle for ${Math.floor(idle / 60)}m.`,
          );
          // Auto-clear the notice after 8 s. The user can still see
          // the stopped state in the UI; the toast is just the
          // explanation of what happened.
          setTimeout(() => setAutoStoppedNotice(null), 8000);
          // Fire the existing stop path so the bucket flushes and
          // the Undo toast appears too — user gets a 5 s window to
          // un-stop if they were just away briefly past the
          // threshold.
          handleStop();
          return;
        }

        // Stage 1: prompt threshold.
        // Reset the ack the moment the user becomes active again
        // (idle drops below half the threshold). Without this the
        // prompt would fire only the first time per session and
        // never warn again no matter how long the user stayed away.
        if (idle < settings.idlePromptSeconds / 2 && idleAck) {
          setIdleAck(false);
        }
        if (idle >= settings.idlePromptSeconds && !idleAck && !idlePromptOpen) {
          setIdlePromptOpen(true);
        }
      } catch {
        // Binding might be momentarily unavailable during shutdown
        // — silently swallow.
      }
    }
    tick();
    const id = setInterval(tick, IDLE_POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
    // attendance + handleStop are referenced inside the tick closure
    // but we deliberately don't add them as deps — the effect
    // already reruns on every isActive change and the closure
    // captures the latest values via React's closure-over-render
    // semantics. eslint can't see that's intentional here.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isActive, idleAck, idlePromptOpen]);

  function dismissSessionBanner() {
    const sessionType = (window as any)._sessionType || "unknown";
    localStorage.setItem(SESSION_BANNER_DISMISSED_KEY, sessionType);
    setSessionBanner("");
  }

  // Patch attendance with the optimistic timestamp so polling doesn't
  // reset the timer.
  //
  // Returns a fresh object — previously we mutated the Wails response
  // in place, which could corrupt cached state inside Wails' IPC layer
  // and surface as "timer jumped backwards" after a network:restored
  // event. See C-FE-1.
  function patchAttendance(d: Attendance | null): Attendance | null {
    if (d?.serverTime) {
      // Every Attendance that flows through the UI goes through here,
      // so this is the single call site that keeps serverClock's
      // offset fresh. Cheap: just a clock sample, no network.
      recordServerTime(d.serverTime);
    }
    if (!d) {
      _optimisticSignInAt = null;
      return null;
    }
    if (d.status !== "SIGNED_IN") {
      _optimisticSignInAt = null;
      return { ...d };
    }
    if (!_optimisticSignInAt) {
      return { ...d };
    }
    const stamped = _optimisticSignInAt;
    return {
      ...d,
      currentSignInAt: stamped,
      sessions: d.sessions?.map((s) =>
        !s.signOutAt ? { ...s, signInAt: stamped } : s
      ),
    };
  }

  useEffect(() => {
    // Clear any stale handlers from a previous mount (Preact fast-refresh
    // or a second mount under StrictMode) BEFORE registering new ones.
    // Wails' EventsOn has no subscription handle, so EventsOff is our
    // only way to guarantee at-most-one handler per event name.
    // See C-FE-2.
    window.runtime.EventsOff("attendance:updated");
    window.runtime.EventsOff("network:error");
    window.runtime.EventsOff("network:restored");

    window.go.main.App.GetMyAttendance()
      .then((d: Attendance | null) => setAttendance(patchAttendance(d)))
      .catch(() => {});
    window.go.main.App.GetWebDashboardURL()
      .then((u: string) => {
        // Validate scheme before rendering as href — the URL crosses
        // the Wails IPC boundary from Go config and could carry a
        // javascript:/data: scheme if a misconfigured build or
        // compromised backend injected one. Anything that isn't an
        // http(s) URL is silently dropped; the footer link simply
        // won't render.
        try {
          const parsed = new URL(u);
          if (parsed.protocol === "https:" || parsed.protocol === "http:") {
            setDashboardURL(u);
          }
        } catch {
          // invalid URL — leave dashboardURL empty, banner won't show
        }
      })
      .catch(() => {});
    // Session capability probe — surfaces Wayland's per-app tracking
    // limit with an actionable message instead of letting the user
    // wonder why their activity report says "Desktop" for everything.
    window.go.main.App.GetSessionInfo()
      .then((s: SessionInfo) => {
        if (!s.limitationMessage) return;
        if (localStorage.getItem(SESSION_BANNER_DISMISSED_KEY) === s.sessionType) return;
        setSessionBanner(s.limitationMessage);
        // Store the current session type on the module so dismiss can
        // scope its record by session type. Prevents a previously-
        // dismissed Wayland banner from hiding a fresh unknown-session
        // warning on the next boot.
        (window as any)._sessionType = s.sessionType;
      })
      .catch(() => {});
    window.runtime.EventsOn("attendance:updated", (d: Attendance | null) =>
      setAttendance(patchAttendance(d ?? null))
    );
    window.runtime.EventsOn("network:error", (msg: string) => {
      setError(msg || "Connection lost. Retrying...");
      // Sustained connectivity loss matters — fire as "error" so
      // both All and Errors-only policies surface it. Background
      // users want to know their timer might not be syncing.
      notify("error", "TaskFlow", msg || "Connection lost. Retrying…");
    });
    window.runtime.EventsOn("network:restored", () => {
      setError("");
      // Recovery is informational — only fires under "All".
      notify("info", "TaskFlow", "Connection restored.");
    });
    return () => {
      window.runtime.EventsOff("attendance:updated");
      window.runtime.EventsOff("network:error");
      window.runtime.EventsOff("network:restored");
      // Clear module-level optimistic timestamp on unmount so a
      // logout → re-login cycle doesn't inherit a stale timer
      // starting-point from the previous session (M-FE-4).
      _optimisticSignInAt = null;
    };
  }, []);

  const sessions = useMemo(() => {
    const raw = attendance?.sessions ?? [];
    if (isActive && attendance?.currentSignInAt)
      return raw.map((s) =>
        !s.signOutAt ? { ...s, signInAt: attendance.currentSignInAt! } : s
      );
    return raw;
  }, [attendance, isActive]);

  // totalHours depends on tickCount when active (for the live timer
  // increment) rather than Date.now(). Using Date.now() as a dep made
  // useMemo recompute on every render — which for a component that
  // re-renders on every keystroke and hover is effectively never
  // memoized at all. See H-FE-1.
  const totalHours = useMemo(
    () => sessions.reduce((sum, s) => sum + getSessionHours(s), 0),
    [sessions, isActive ? tickCount : 0]
  );

  const groupedTasks = useMemo(
    () => groupSessionsByTask(sessions),
    [sessions, isActive ? tickCount : 0]
  );

  async function handleStart(data: StartTimerData) {
    if (!navigator.onLine) { setError("No internet connection."); return; }
    setLoading(true);
    setError("");
    // Optimistic stamp covers ONLY the moment between click and the
    // server's SignIn response — typically 100 ms – 2 s. The instant
    // the server comes back with its canonical signInAt we clear the
    // stamp and all downstream renders use the server's value, so
    // this desktop shows the same "elapsed" as the web app (which
    // pulls that same server signInAt on its poll).
    //
    // Before this change the stamp persisted for the entire session
    // and every subsequent poll was patched with the click-time; two
    // clients therefore showed times that drifted by the sign-in
    // RTT. For short sessions the drift was invisible; for users
    // running the desktop app for the full day it compounded into
    // minutes.
    const t0 = new Date().toISOString();
    _optimisticSignInAt = t0;
    try {
      const r = await window.go.main.App.SignIn(data);
      // Clear BEFORE patchAttendance so the call uses the server's
      // signInAt as the authoritative source from this point on.
      _optimisticSignInAt = null;
      setAttendance(patchAttendance(r));
    } catch (err: any) {
      const raw = typeof err === "string" ? err : err?.message || "";
      if (raw.includes("already signed in")) {
        _optimisticSignInAt = null;
        // Nested call MUST have its own .catch so a throw here doesn't
        // escape handleStart — otherwise the finally would still run
        // setLoading(false), but the user would see a toast from the
        // uncaught rejection. See C-FE-3.
        const c = await window.go.main.App.GetMyAttendance().catch(() => null);
        if (c) setAttendance(c);
      } else {
        _optimisticSignInAt = null;
        setError(friendlyError(err));
      }
    } finally {
      // Runs on every code path — success, "already signed in" recovery,
      // and hard error. Do not move setLoading(false) out of finally.
      setLoading(false);
    }
  }

  async function handleStop() {
    if (!navigator.onLine) { setError("No internet connection."); return; }
    // Capture the task that's about to be stopped — needed to drive
    // the Undo affordance below. If the stop succeeds, this is the
    // payload Undo will replay; if it fails, the timer never
    // actually stopped, so we don't show the toast.
    const cur = attendance?.currentTask;
    const curSess = sessions.find((s) => !s.signOutAt);
    const undoPayload: StartTimerData | null = cur
      ? {
          taskId: cur.taskId || "",
          projectId: cur.projectId || "",
          taskTitle: cur.taskTitle,
          projectName: cur.projectName || "",
          description: curSess?.description || cur.taskTitle,
        }
      : null;

    setLoading(true);
    setError("");
    _optimisticSignInAt = null;
    try {
      setAttendance(await window.go.main.App.SignOut());
      if (undoPayload) {
        setUndoableStop(undoPayload);
        // Auto-clear after the grace window. If the user already
        // clicked Undo in the meantime the state will already be
        // null and this is a no-op.
        setTimeout(() => {
          setUndoableStop((cur) => (cur === undoPayload ? null : cur));
        }, 5000);
      }
    } catch (err: any) {
      setError(friendlyError(err));
    } finally {
      setLoading(false);
    }
  }

  async function handleUndoStop() {
    if (!undoableStop) return;
    const payload = undoableStop;
    setUndoableStop(null);
    // Reuse handleStart so the optimistic-stamp / error-handling /
    // already-signed-in recovery logic is shared.
    await handleStart(payload);
  }

  function handleResume(t: GroupedTask) {
    handleStart({
      taskId: t.taskId || "",
      projectId: t.projectId || "",
      taskTitle: t.taskTitle,
      projectName: t.projectName || "",
      description: t.description || t.taskTitle,
    });
  }

  // ─── Keyboard shortcuts ─────────────────────────────────────────
  // Power-user affordances. The chord layer is OS-aware (Mod = Ctrl
  // on Windows/Linux, Cmd on macOS). Chords that target inputs (?,
  // Mod+/, Esc) override the default in-input suppression; chords
  // that act on the timer (Stop, Sign Out) do not — typing in a text
  // field shouldn't accidentally end your session.
  const shortcutDefs = [
    {
      chord: "?",
      label: "Show keyboard shortcuts",
      whenInInput: true,
      handler: () => setHelpOpen(true),
    },
    {
      chord: "Escape",
      label: "Close overlay / dismiss banner",
      whenInInput: true,
      handler: () => {
        if (helpOpen) setHelpOpen(false);
        else if (sessionBanner) dismissSessionBanner();
      },
    },
    {
      chord: "Mod+/",
      label: "Focus the description field",
      whenInInput: true,
      handler: () => {
        const el = document.querySelector<HTMLInputElement>(
          'input[placeholder="What are you working on?"]',
        );
        el?.focus();
        el?.select();
      },
    },
    {
      chord: "Mod+r",
      label: "Refresh tasks & projects",
      whenInInput: true,
      handler: () => {
        document
          .querySelector<HTMLButtonElement>('button[aria-label="Refresh tasks"]')
          ?.click();
      },
    },
    {
      chord: "Mod+.",
      label: "Stop the running timer",
      whenInInput: false,
      handler: () => {
        if (isActive) handleStop();
      },
    },
    {
      chord: "Mod+l",
      label: "Sign out",
      whenInInput: false,
      handler: () => onLogout(),
    },
  ];
  useShortcuts(shortcutDefs);

  /* ═══ Shared overlay markup ═══ */
  const helpOverlay = (
    <ShortcutHelp
      open={helpOpen}
      onClose={() => setHelpOpen(false)}
      shortcuts={shortcutDefs.map((s) => ({ chord: s.chord, label: s.label }))}
    />
  );

  // SessionInspect: filter the full session list down to the entries
  // matching the inspected grouped-task. The grouping key matches
  // groupSessionsByTask's logic so we re-derive the same filter here.
  const inspectedTask = inspectingTaskKey
    ? groupedTasks.find((t) => taskKeyOf(t) === inspectingTaskKey) ?? null
    : null;
  const inspectedSessions = inspectedTask
    ? sessions.filter((s) => sessionKeyOf(s) === inspectingTaskKey)
    : [];
  const inspectOverlay = (
    <SessionInspect
      open={!!inspectedTask}
      onClose={() => setInspectingTaskKey(null)}
      sessions={inspectedSessions}
      taskTitle={inspectedTask?.taskTitle || ""}
      projectName={inspectedTask?.projectName || ""}
    />
  );

  function handleInspect(t: GroupedTask) {
    setInspectingTaskKey(taskKeyOf(t));
  }

  const idleOverlay = (
    <>
      {idlePromptOpen && (
        <IdlePrompt
          idleSeconds={idleSecondsForPrompt}
          onKeepTracking={() => {
            setIdleAck(true);
            setIdlePromptOpen(false);
          }}
          onStopTimer={() => {
            setIdlePromptOpen(false);
            handleStop();
          }}
        />
      )}
      {autoStoppedNotice && (
        // Auto-stopped toast — non-dismissible; auto-clears after
        // 8 s. Sits at the top of the body so it doesn't fight with
        // the Undo toast (bottom-anchored). The Undo toast still
        // appears, giving the user a 5 s window to revive the
        // timer if they were just briefly past 15 min.
        <div
          role="status"
          class={cn(
            "absolute left-1/2 -translate-x-1/2 z-30",
            "top-3 px-3 py-2 max-w-[340px] w-[calc(100%-24px)]",
            "rounded-lg border border-amber-500/30 bg-amber-500/[0.10]",
            "shadow-lg",
            "animate-in slide-in-from-top-2 fade-in",
            "flex items-start gap-2.5",
          )}
        >
          <svg class="mt-0.5 h-4 w-4 flex-shrink-0 text-amber-600 dark:text-amber-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <circle cx="12" cy="12" r="10" />
            <path d="M12 6v6l4 2" />
          </svg>
          <div class="min-w-0 flex-1">
            <p class="text-[12px] font-semibold text-amber-700 dark:text-amber-300 leading-tight">
              Timer auto-stopped
            </p>
            <p class="text-[10.5px] text-amber-700/85 dark:text-amber-300/85 leading-snug mt-0.5">
              <span class="font-semibold">{autoStoppedNotice.taskTitle}</span> was idle for{" "}
              <span class="font-semibold tabular-nums">{autoStoppedNotice.minutes}m</span>.
            </p>
          </div>
        </div>
      )}
    </>
  );

  // Undo toast — fixed at the bottom of the window, slides in when
  // undoableStop is non-null, slides out at the 5 s timeout or on
  // user action. Lives at the TimerView level so it persists across
  // the active→stopped branch transition that handleStop triggers.
  const undoToast = undoableStop ? (
    <div
      role="status"
      class={cn(
        "absolute left-1/2 -translate-x-1/2 z-30",
        "bottom-3 px-3 py-2 max-w-[320px] w-[calc(100%-24px)]",
        "rounded-lg border border-border bg-popover text-popover-foreground shadow-xl",
        "animate-in slide-in-from-bottom-2 fade-in",
        "flex items-center gap-3",
      )}
    >
      <svg class="h-4 w-4 flex-shrink-0 text-muted-foreground" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <rect x="6" y="6" width="12" height="12" rx="1.5" />
      </svg>
      <span class="flex-1 text-[12px] text-foreground">
        Timer stopped.
        <span class="text-muted-foreground"> Changed your mind?</span>
      </span>
      <button
        type="button"
        onClick={handleUndoStop}
        class={cn(
          "inline-flex items-center gap-1 px-2 py-1 rounded-md",
          "text-[11px] font-semibold text-primary",
          "hover:bg-primary/10 transition-colors",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        )}
      >
        <svg class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M3 7v6h6" />
          <path d="M21 17a9 9 0 0 0-15-6.7L3 13" />
        </svg>
        Undo
      </button>
      <button
        type="button"
        onClick={() => setUndoableStop(null)}
        class="h-5 w-5 inline-flex items-center justify-center rounded text-muted-foreground hover:text-foreground hover:bg-accent/60 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        aria-label="Dismiss"
      >
        <svg class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
          <path d="M6 6l12 12M6 18L18 6" />
        </svg>
      </button>
    </div>
  ) : null;

  /* ═══ ACTIVE ═══ */
  if (isActive && attendance) {
    const cur = attendance.currentTask;
    const curSess = sessions.find((s) => !s.signOutAt);
    return (
      <Shell
        user={user}
        onLogout={onLogout}
        dashboardURL={dashboardURL}
        todayHours={totalHours}
        bottom={
          <>
            <ErrorBar error={error} />
            <SessionBanner message={sessionBanner} onDismiss={dismissSessionBanner} />
            {/* Switch-Task strip — emerald top border so it visually
                belongs to the recording card above instead of reading
                as an unrelated section (P1-13). The accent stays
                subtle so the focus remains on the timer hero. */}
            <div class="px-3 pt-2.5 pb-3 border-t border-emerald-500/30 bg-emerald-500/[0.025] dark:bg-emerald-500/[0.05]">
              <p class="text-[9.5px] font-semibold uppercase tracking-[0.10em] mb-2 text-emerald-700/85 dark:text-emerald-400/85">
                Switch Task
              </p>
              <TaskSelector
                onStart={handleStart}
                loading={loading}
                switching={
                  attendance?.currentTask && attendance?.currentSignInAt
                    ? {
                        currentTaskTitle: attendance.currentTask.taskTitle,
                        runningSince: attendance.currentSignInAt,
                      }
                    : null
                }
              />
            </div>
          </>
        }
      >
        {/* Live timer card — emerald accent indicates recording state but
            kept restrained so the timer numerals (not the chrome) are
            the strongest visual element. The `recording-ignite`
            keyframe (defined in main.css) does a 250ms scale-in +
            glow pulse so transitioning from stopped→active feels
            like the timer "lit up" instead of merely appearing. */}
        <Card class="mx-3 mt-3 overflow-hidden border-emerald-500/30 bg-gradient-to-b from-emerald-500/[0.07] to-transparent dark:from-emerald-500/[0.10] shadow-[0_1px_2px_rgba(15,17,35,0.04),0_8px_20px_-12px_rgba(16,185,129,0.18)] recording-ignite">
          <div class="px-4 pt-3.5 pb-3.5 text-center">
            {/* Recording badge — pill with pulsing dot */}
            <div class="inline-flex items-center gap-1.5 px-2 py-0.5 mb-3 rounded-full bg-emerald-500/12 dark:bg-emerald-500/18 border border-emerald-500/25">
              <span class="relative flex h-1.5 w-1.5" aria-hidden="true">
                <span class="animate-ping absolute h-full w-full rounded-full bg-emerald-500 opacity-70" />
                <span class="relative rounded-full h-1.5 w-1.5 bg-emerald-500" />
              </span>
              <span class="text-[9px] font-semibold uppercase tracking-[0.14em] text-emerald-700 dark:text-emerald-300">
                Recording
              </span>
            </div>

            {/* Timer numerals — increase letter weight + tight tracking
                so the digits read as a hero, not body text. */}
            {attendance.currentSignInAt && (
              <Timer
                startTime={attendance.currentSignInAt}
                class="block font-mono font-bold tracking-[-0.02em] text-[38px] leading-none text-emerald-700 dark:text-emerald-300 tabular-nums"
              />
            )}

            {/* Task info — title is the secondary anchor; meta sits in
                muted color so it doesn't compete with the numerals. */}
            {(() => {
              const title = cur?.taskTitle || "Working";
              const meta = [
                cur?.projectName,
                curSess?.description && `· ${curSess.description}`,
              ]
                .filter(Boolean)
                .join(" ");
              return (
                <>
                  <p
                    class="text-[13px] font-semibold mt-3 truncate text-foreground px-2 leading-tight tracking-[-0.005em]"
                    title={title}
                  >
                    {title}
                  </p>
                  {meta && (
                    <p
                      class="text-[10.5px] truncate text-muted-foreground mt-0.5 px-2 leading-snug"
                      title={meta}
                    >
                      {meta}
                    </p>
                  )}
                </>
              );
            })()}

            {/* Stop button — outline-style on the green field so the
                emerald palette stays dominant. The destructive intent
                is conveyed by the icon + label, not by a red flood. */}
            <Button
              variant="default"
              class={cn(
                "mt-3.5 w-full h-9 text-[12.5px] font-semibold gap-2",
                "bg-card hover:bg-card text-destructive border border-destructive/25 hover:border-destructive/40",
                "shadow-sm hover:shadow",
                "active:scale-[.985]",
              )}
              onClick={handleStop}
              disabled={loading}
            >
              {loading ? (
                <span class="opacity-80">Stopping…</span>
              ) : (
                <>
                  <StopIcon />
                  Stop Timer
                </>
              )}
            </Button>
          </div>

          {/* Stats strip */}
          <div class="flex items-center justify-between px-4 py-2 border-t border-emerald-500/20 bg-emerald-500/[0.04] dark:bg-emerald-500/[0.08]">
            <span class="text-[10px] font-medium text-muted-foreground tracking-[0.005em]">
              {sessions.length} session{sessions.length !== 1 && "s"} today
            </span>
            <span class="text-[11px] font-bold font-mono tabular-nums text-foreground/85">
              {formatDuration(totalHours)}
            </span>
          </div>
        </Card>

        <SessionBlock tasks={groupedTasks} onResume={handleResume} onInspect={handleInspect} loading={loading} goalHours={settings.dailyGoalHours} />
        {helpOverlay}
        {inspectOverlay}
        {undoToast}
        {idleOverlay}
      </Shell>
    );
  }

  /* ═══ STOPPED ═══ */
  return (
    <Shell
      user={user}
      onLogout={onLogout}
      dashboardURL={dashboardURL}
      todayHours={totalHours}
      bottom={
        <>
          <ErrorBar error={error} />
          <SessionBanner message={sessionBanner} onDismiss={dismissSessionBanner} />
          <div class="px-3 pt-2.5 pb-3 border-t border-border bg-card">
            <TaskSelector onStart={handleStart} loading={loading} />
          </div>
        </>
      }
    >
      <div class="flex items-end justify-between gap-3 px-4 pt-4 pb-3.5 border-b border-border">
        <div class="min-w-0">
          <p class="text-[9.5px] font-semibold uppercase tracking-[0.12em] text-muted-foreground/80 leading-none">
            Today
          </p>
          <p class="text-sm font-semibold text-foreground leading-tight mt-1.5">
            Time Tracker
          </p>
          <p class="text-[11px] text-muted-foreground mt-0.5 leading-tight">
            {sessions.length > 0
              ? `${sessions.length} session${sessions.length !== 1 ? "s" : ""} logged`
              : "No sessions yet"}
          </p>
        </div>
        <span
          class={cn(
            "font-mono tabular-nums leading-none tracking-tight",
            sessions.length > 0
              ? "text-[22px] font-bold text-foreground"
              : "text-[20px] font-semibold text-muted-foreground/35",
          )}
          style={{ fontFeatureSettings: '"tnum","zero"' }}
        >
          {sessions.length > 0 ? formatDuration(totalHours) : "00:00:00"}
        </span>
      </div>

      {sessions.length === 0 && (
        // Refined empty state: solid soft surface + a small leading icon
        // chip instead of a dashed-border placeholder. Reads as "you're
        // ready" rather than "this section is missing content".
        <div class="mx-3 mt-3 rounded-lg border border-border bg-muted/30 px-4 py-5 flex items-start gap-3">
          <span class="flex h-7 w-7 flex-shrink-0 items-center justify-center rounded-full bg-primary/10 text-primary">
            <svg class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <circle cx="12" cy="12" r="10" />
              <path d="M12 6v6l4 2" />
            </svg>
          </span>
          <div class="min-w-0">
            <p class="text-[12px] font-semibold text-foreground leading-tight">
              Ready when you are
            </p>
            <p class="text-[10.5px] text-muted-foreground mt-0.5 leading-snug">
              Describe what you're working on and pick a task to start tracking.
            </p>
          </div>
        </div>
      )}

      <SessionBlock tasks={groupedTasks} onResume={handleResume} onInspect={handleInspect} loading={loading} goalHours={settings.dailyGoalHours} />
      {helpOverlay}
      {inspectOverlay}
      {undoToast}
      {idleOverlay}
    </Shell>
  );
}

/* ════════════════ Shell ════════════════ */

function Shell({
  user,
  onLogout,
  children,
  bottom,
  dashboardURL,
  todayHours,
}: {
  user: User;
  onLogout: () => void;
  children: any;
  bottom?: any;
  dashboardURL?: string;
  /** Drives the avatar progress ring + the "X.Xh of 8h" readout in
   *  the menu. Computed by TimerView from the current attendance
   *  state; passed in here so Shell stays presentation-only. */
  todayHours: number;
}) {
  // Shell subscribes to settings independently of TimerView so the
  // avatar menu's daily-goal display + theme toggle stay reactive
  // without prop-threading. Both components observe the same
  // localStorage-backed store.
  const [settings] = useSettings();
  const { isDark, toggle } = useTheme();
  const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(null);
  const [updating, setUpdating] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  // App version — fetched once on mount, surfaced in the avatar menu
  // footer. Useful in support tickets ("running v1.0.3-staging").
  const [appVersion, setAppVersion] = useState<string>("");
  useEffect(() => {
    window.go.main.App.GetAppVersion().then(setAppVersion).catch(() => {});
  }, []);
  // packageManagedNotice is populated when the Go updater refuses to
  // auto-install because this binary belongs to a system package
  // manager (.deb/.rpm/snap). The button swaps to a read-only banner
  // explaining the user should run `apt upgrade` instead of making
  // the app loop on an install that will always fail. See V3-Mdeb.
  const [packageManagedNotice, setPackageManagedNotice] = useState<string | null>(null);

  // Listen for update:available event from Go backend. Clear any stale
  // handler first (C-FE-2 pattern — defensive against Preact
  // fast-refresh / double mount).
  useEffect(() => {
    window.runtime.EventsOff("update:available");
    window.runtime.EventsOn("update:available", (info: UpdateInfo) => {
      if (info?.available) {
        setUpdateInfo(info);
        // Update offers are informational — only fire when policy
        // is "All". Errors-only suppresses because there's no
        // urgent action; the in-app banner is the primary cue.
        notify("info", "TaskFlow update available", `Version ${info.version} is ready to install.`);
      }
    });
    window.runtime.EventsOff("update:package-managed");
    window.runtime.EventsOn(
      "update:package-managed",
      (payload: { version?: string; message?: string }) => {
        setPackageManagedNotice(
          payload?.message ||
            "A new version is available — use your system package manager to update.",
        );
        setUpdating(false);
      },
    );
    return () => {
      window.runtime.EventsOff("update:available");
      window.runtime.EventsOff("update:package-managed");
    };
  }, []);

  async function handleUpdate() {
    if (!updateInfo) return;
    setUpdating(true);
    try {
      // InstallUpdate takes no arguments — the Go side re-fetches the
      // release info internally so the download URL never crosses IPC.
      await window.go.main.App.InstallUpdate();
    } catch {
      setUpdating(false);
    }
  }

  return (
    <div class="flex flex-col h-full bg-background">
      {/* Header — avatar consolidates: identity, today's progress
          (ring + readout), theme toggle, dashboard link, sign-out,
          version. Replaces the previous stacked icons-and-buttons
          row. */}
      <header class="flex items-center justify-between gap-2 px-3 py-2 bg-card border-b border-border">
        <AvatarMenu
          user={user}
          todayHours={todayHours}
          goalHours={settings.dailyGoalHours}
          isDark={isDark}
          onToggleTheme={toggle}
          onLogout={onLogout}
          onOpenSettings={() => setSettingsOpen(true)}
          dashboardURL={dashboardURL}
          version={appVersion}
        />
      </header>
      <SettingsDrawer open={settingsOpen} onClose={() => setSettingsOpen(false)} />

      {packageManagedNotice ? (
        // Informational banner — read-only. No action button because the
        // user has to run their package manager themselves; making it
        // look "actionable" would be misleading.
        <div class="flex items-start gap-2 px-3 py-2 bg-primary/[0.06] border-b border-primary/15">
          <span class="mt-0.5 inline-flex h-3.5 w-3.5 flex-shrink-0 items-center justify-center text-primary" aria-hidden="true">
            <svg class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <circle cx="12" cy="12" r="10" />
              <path d="M12 8v4M12 16h.01" stroke-linecap="round" />
            </svg>
          </span>
          <p class="text-[11px] leading-[1.45] text-primary/90 font-medium">{packageManagedNotice}</p>
        </div>
      ) : (
        updateInfo && (
          // Actionable update banner — accent strip, version chip, single
          // primary CTA. Higher visual priority than the package-managed
          // notice because the user can act directly from here.
          <div class="flex items-center justify-between gap-3 px-3 py-2 bg-primary/[0.06] border-b border-primary/15">
            <div class="flex items-center gap-2 min-w-0">
              <span class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-primary/15 text-primary text-[9px] font-bold tracking-[0.08em] uppercase tabular-nums">
                <span class="relative flex h-1.5 w-1.5" aria-hidden="true">
                  <span class="animate-ping absolute h-full w-full rounded-full bg-primary opacity-60" />
                  <span class="relative h-1.5 w-1.5 rounded-full bg-primary" />
                </span>
                Update
              </span>
              <p class="text-[11px] font-medium text-foreground truncate">
                <span class="text-muted-foreground">v{updateInfo.currentVersion}</span>
                <span class="text-muted-foreground/50 mx-1.5">→</span>
                <span class="font-semibold text-primary">v{updateInfo.version}</span>
              </p>
            </div>
            <Button
              size="sm"
              class="h-7 px-3 text-[11px] font-semibold flex-shrink-0"
              onClick={handleUpdate}
              disabled={updating}
            >
              {updating ? "Updating…" : "Install"}
            </Button>
          </div>
        )
      )}

      <div class="flex-1 overflow-y-auto">{children}</div>

      {bottom}

      <footer class="px-3 py-1.5 flex items-center justify-between bg-card border-t border-border">
        <div class="flex items-center gap-1.5 select-none">
          <TaskFlowLogo size={14} />
          <span class="text-[10px] font-extrabold tracking-tight text-muted-foreground/80 leading-none">
            Task<span class="text-primary">Flow</span>
          </span>
        </div>
        {/* Quiet hint about the help overlay — replaces the old
            dashboard link, which moved into the avatar menu. The "?"
            chord works anywhere in the app. The kbd needs explicit
            sizing because <kbd> ships with a UA-default font and
            line-height that ignores our text-[9.5px] parent. Without
            this, the chip towers above the surrounding text. */}
        <span class="text-[9.5px] text-muted-foreground/70 inline-flex items-center gap-1 leading-none">
          Press
          <kbd
            class={cn(
              "font-mono font-semibold rounded border border-border bg-muted text-foreground/80",
              "inline-flex items-center justify-center",
              "h-[14px] min-w-[14px] px-1 text-[9.5px] leading-none",
            )}
          >
            ?
          </kbd>
          for shortcuts
        </span>
      </footer>
    </div>
  );
}

/* ════════════════ Sessions ════════════════ */

function SessionBlock({
  tasks,
  onResume,
  onInspect,
  loading,
  goalHours,
}: {
  tasks: GroupedTask[];
  onResume: (t: GroupedTask) => void;
  onInspect: (t: GroupedTask) => void;
  loading: boolean;
  /** Daily goal in hours — drives the celebratory pulse when total
   *  reaches it. Threaded down so the block doesn't have to import
   *  the settings hook itself. */
  goalHours: number;
}) {
  if (tasks.length === 0) return null;
  const total = tasks.reduce((s, t) => s + t.totalHours, 0);
  // Goal-pulse: subtle emerald glow on the total when the user has
  // hit (or surpassed) their daily goal. Quiet celebration — no
  // confetti, no bounce, just a gentle text-shadow pulse loop
  // defined in main.css. P2-18.
  const goalReached = total >= goalHours;

  return (
    <Card class="mx-3 mt-3 overflow-hidden">
      {/* Lighter eyebrow — 0.10em letter-spacing reads more refined at
          this size than 0.14em, which felt heavy for the body width. */}
      <div class="flex items-center justify-between px-3 py-2 bg-muted/40 border-b border-border">
        <span class="text-[9.5px] font-semibold uppercase tracking-[0.10em] text-muted-foreground/85">
          Today's Sessions
        </span>
        <span class="text-[10px] font-medium text-muted-foreground/80 tabular-nums">
          {tasks.length} task{tasks.length !== 1 && "s"}
        </span>
      </div>
      <div>
        {tasks.map((t, i) => (
          <TaskRow
            key={i}
            task={t}
            onResume={() => onResume(t)}
            onInspect={() => onInspect(t)}
            loading={loading}
          />
        ))}
      </div>
      <div class="flex items-center justify-between px-3 py-2 bg-muted/40 border-t border-border">
        <span class="text-[9.5px] font-semibold uppercase tracking-[0.10em] text-muted-foreground/85 inline-flex items-center gap-1.5">
          Total
          {goalReached && (
            <span
              class="inline-flex items-center gap-1 rounded-full bg-emerald-500/15 px-1.5 py-0.5 text-[8.5px] font-bold tracking-[0.06em] text-emerald-700 dark:text-emerald-300"
              title={`Daily goal of ${goalHours}h reached`}
            >
              <svg class="h-2.5 w-2.5" fill="currentColor" viewBox="0 0 20 20" aria-hidden="true">
                <path
                  fill-rule="evenodd"
                  d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z"
                  clip-rule="evenodd"
                />
              </svg>
              GOAL
            </span>
          )}
        </span>
        <span
          class={cn(
            "text-[13px] font-bold font-mono tabular-nums tracking-tight transition-colors",
            goalReached ? "text-emerald-700 dark:text-emerald-300 goal-pulse" : "text-foreground",
          )}
          style={{ fontFeatureSettings: '"tnum","zero"' }}
        >
          {formatDuration(total)}
        </span>
      </div>
    </Card>
  );
}

/* ════════════════ Task Row ════════════════ */

interface GroupedTask {
  taskTitle: string;
  projectName: string;
  taskId: string | null;
  projectId: string | null;
  description: string | null;
  totalHours: number;
  sessionCount: number;
  isRunning: boolean;
}

function TaskRow({
  task,
  onResume,
  onInspect,
  loading,
}: {
  task: GroupedTask;
  onResume: () => void;
  onInspect: () => void;
  loading: boolean;
}) {
  return (
    <button
      type="button"
      onClick={(e) => {
        // Running rows aren't resumable (you can't start what's
        // already running), but they ARE inspectable. Click on a
        // running row opens the detail drawer; click on a stopped
        // row resumes. Right-click always opens the drawer.
        if (task.isRunning) {
          e.preventDefault();
          onInspect();
          return;
        }
        onResume();
      }}
      onContextMenu={(e) => {
        // Right-click → inspect drawer. Suppress the browser's
        // default context menu (which only ever offered "Reload" and
        // similar useless options inside Wails). Long-press isn't
        // wired separately — desktops are mouse-first; touch users
        // can still tap the row to resume and open the menu via the
        // (future) row hover affordance.
        e.preventDefault();
        onInspect();
      }}
      disabled={loading}
      class={cn(
        "group relative w-full flex items-center gap-2.5 px-3 py-2.5 border-b border-border/60 last:border-0",
        "text-left transition-all duration-150",
        "hover:bg-accent/40",
        "focus-visible:outline-none focus-visible:bg-accent/60",
        "disabled:cursor-not-allowed",
        // Running rows aren't "disabled-looking" — they're the active
        // state, just non-clickable. Don't fade them.
        loading && !task.isRunning && "opacity-50",
      )}
      title={
        task.isRunning
          ? `${task.taskTitle} — currently active · right-click for details`
          : `Resume ${task.taskTitle} · right-click for details`
      }
      aria-label={task.isRunning ? `${task.taskTitle} active` : `Resume ${task.taskTitle}`}
    >
      {/* Leading state chip: green animated dot for the running task,
          play glyph for resumable rows. Subtle ring on hover gives the
          row a tactile feedback without a heavy bg shift. */}
      <div
        class={cn(
          "flex-shrink-0 w-7 h-7 rounded-full flex items-center justify-center",
          "ring-1 ring-transparent transition-all duration-150",
          task.isRunning
            ? "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 ring-emerald-500/25"
            : "bg-primary/10 text-primary group-hover:bg-primary/15 group-hover:ring-primary/20",
        )}
      >
        {task.isRunning ? (
          <span class="relative flex h-2 w-2" aria-hidden="true">
            <span class="animate-ping absolute h-full w-full rounded-full bg-emerald-500 opacity-70" />
            <span class="relative h-2 w-2 rounded-full bg-emerald-500" />
          </span>
        ) : (
          <svg
            class="w-3 h-3 ml-0.5 transition-transform duration-150 group-hover:scale-110"
            fill="currentColor"
            viewBox="0 0 24 24"
            aria-hidden="true"
          >
            <path d="M8 5v14l11-7z" />
          </svg>
        )}
      </div>
      <div class="min-w-0 flex-1">
        <p
          class={cn(
            "text-[12px] font-semibold truncate leading-tight tracking-[-0.005em]",
            task.isRunning ? "text-foreground" : "text-foreground",
          )}
          title={task.taskTitle}
        >
          {task.taskTitle}
        </p>
        <p
          class="text-[10.5px] truncate leading-tight text-muted-foreground mt-0.5 flex items-center gap-1"
          title={[
            task.projectName,
            task.description && task.description !== task.taskTitle ? task.description : null,
            `${task.sessionCount} sessions`,
          ]
            .filter(Boolean)
            .join(" · ")}
        >
          {/* Per-project color dot — deterministic from project name
              until the backend ships a real color field. P2-22. */}
          {task.projectName && task.projectName !== "Direct" && (
            <span
              class="w-1.5 h-1.5 flex-shrink-0 rounded-full"
              style={{ background: colorForProject(task.projectName) }}
              aria-hidden="true"
            />
          )}
          <span class="truncate">
            {task.projectName}
            {task.description && task.description !== task.taskTitle && (
              <span class="text-muted-foreground/85"> · {task.description}</span>
            )}
            <span class="text-muted-foreground/55 tabular-nums"> · {task.sessionCount}×</span>
          </span>
        </p>
      </div>
      <span
        class={cn(
          "text-[12.5px] font-bold font-mono tabular-nums flex-shrink-0 ml-2 leading-none tracking-tight",
          task.isRunning ? "text-emerald-600 dark:text-emerald-400" : "text-foreground/85",
        )}
        style={{ fontFeatureSettings: '"tnum","zero"' }}
      >
        {formatDuration(task.totalHours)}
      </span>
    </button>
  );
}

function ErrorBar({ error }: { error: string }) {
  if (!error) return null;
  return (
    <div
      role="alert"
      class="mx-3 mb-2 text-xs p-2 rounded-md bg-destructive/10 border border-destructive/30 text-destructive"
    >
      {error}
    </div>
  );
}

// SessionBanner surfaces OS display-server limitations (Wayland per-app
// tracking, non-systemd session, etc.) that aren't errors but which the
// user should know about. Dismissal persists per session-type, so a
// Wayland user who re-logs into X11 will see the X11 (empty) banner
// path, not a stale dismissal from Wayland.
function SessionBanner({ message, onDismiss }: { message: string; onDismiss: () => void }) {
  if (!message) return null;
  return (
    <div
      role="status"
      class="mx-3 mb-2 text-xs p-2 rounded-md flex items-start gap-2 bg-amber-500/10 border border-amber-500/30 text-amber-700 dark:text-amber-300"
    >
      <span class="flex-1">{message}</span>
      <button
        onClick={onDismiss}
        class="opacity-60 hover:opacity-100 transition-opacity focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded"
        aria-label="Dismiss"
      >
        ×
      </button>
    </div>
  );
}

/* ════════════════ Icons ════════════════ */

function StopIcon() {
  return (
    <svg class="w-3.5 h-3.5 fill-current" viewBox="0 0 24 24">
      <rect x="6" y="6" width="12" height="12" rx="1.5" />
    </svg>
  );
}

/* ════════════════ Utils ════════════════ */

function getSessionHours(session: AttendanceSession): number {
  // Active session: tick against serverNow() (= Date.now() + offset)
  // so the running total agrees across devices regardless of local
  // OS clock accuracy. Closed sessions use their stored timestamps,
  // which are already server-canonical.
  if (!session.signOutAt) return (serverNow() - new Date(session.signInAt).getTime()) / 3600000;
  if (session.hours && session.hours > 0) return session.hours;
  return (new Date(session.signOutAt).getTime() - new Date(session.signInAt).getTime()) / 3600000;
}

// Single source of truth for "which session belongs to which grouped
// task" — used both by groupSessionsByTask (for the rendered list)
// and by the inspect-drawer filter (so right-click on row N shows
// exactly the sessions that contributed to row N's total).
function sessionKeyOf(s: AttendanceSession): string {
  return s.taskId || s.taskTitle || s.description || "general";
}
function taskKeyOf(t: GroupedTask): string {
  return t.taskId || t.taskTitle || t.description || "general";
}

function groupSessionsByTask(sessions: AttendanceSession[]): GroupedTask[] {
  const map = new Map<string, GroupedTask>();
  for (const s of sessions) {
    const key = sessionKeyOf(s);
    const hrs = getSessionHours(s);
    const isRunning = !s.signOutAt;
    const existing = map.get(key);
    if (existing) {
      existing.totalHours += hrs;
      existing.sessionCount++;
      existing.isRunning = existing.isRunning || isRunning;
      if (s.description && !existing.description) existing.description = s.description;
    } else {
      map.set(key, {
        taskTitle: s.taskTitle || s.description || "General",
        projectName: s.projectName || "Direct",
        taskId: s.taskId, projectId: s.projectId, description: s.description,
        totalHours: hrs, sessionCount: 1, isRunning,
      });
    }
  }
  return Array.from(map.values());
}
