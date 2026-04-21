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
import { useTheme } from "../lib/useTheme";
import { friendlyError } from "../lib/errors";
import { Button } from "./ui/Button";
import { Card } from "./ui/Card";
import { cn } from "../lib/cn";

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

export function TimerView({ user, onLogout }: TimerViewProps) {
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

  useEffect(() => {
    if (!isActive) return;
    const i = setInterval(() => setTick((t) => t + 1), 1000);
    return () => clearInterval(i);
  }, [isActive]);

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
    });
    window.runtime.EventsOn("network:restored", () => {
      setError("");
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
    const t0 = new Date().toISOString();
    _optimisticSignInAt = t0; // Persist across polling updates
    try {
      const r = await window.go.main.App.SignIn(data);
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
    setLoading(true);
    setError("");
    _optimisticSignInAt = null;
    try {
      setAttendance(await window.go.main.App.SignOut());
    } catch (err: any) {
      setError(friendlyError(err));
    } finally {
      setLoading(false);
    }
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

  /* ═══ ACTIVE ═══ */
  if (isActive && attendance) {
    const cur = attendance.currentTask;
    const curSess = sessions.find((s) => !s.signOutAt);
    return (
      <Shell
        user={user}
        onLogout={onLogout}
        dashboardURL={dashboardURL}
        bottom={
          <>
            <ErrorBar error={error} />
            <SessionBanner message={sessionBanner} onDismiss={dismissSessionBanner} />
            <div class="px-3 py-2.5 border-t border-border bg-card">
              <p class="text-[10px] font-bold uppercase tracking-widest mb-1.5 text-muted-foreground">
                Switch Task
              </p>
              <TaskSelector onStart={handleStart} loading={loading} />
            </div>
          </>
        }
      >
        {/* Live timer card */}
        <Card class="mx-3 mt-3 overflow-hidden border-emerald-500/30 bg-emerald-500/5 dark:bg-emerald-500/10">
          <div class="px-4 pt-3 pb-2.5 text-center">
            <div class="flex items-center justify-center gap-2 mb-1.5">
              <span class="relative flex h-2 w-2">
                <span class="animate-ping absolute h-full w-full rounded-full bg-emerald-500 opacity-60" />
                <span class="relative rounded-full h-2 w-2 bg-emerald-500" />
              </span>
              <span class="text-[10px] font-semibold uppercase tracking-widest text-emerald-700 dark:text-emerald-300">
                Recording
              </span>
            </div>

            {attendance.currentSignInAt && (
              <Timer
                startTime={attendance.currentSignInAt}
                class="font-mono font-bold tracking-tight text-[34px] text-emerald-700 dark:text-emerald-300"
              />
            )}

            <p class="text-xs font-semibold mt-1.5 truncate text-foreground">
              {cur?.taskTitle || "Working"}
            </p>
            <p class="text-[10px] truncate text-muted-foreground">
              {cur?.projectName}
              {curSess?.description && <span class="italic"> — {curSess.description}</span>}
            </p>

            <Button
              variant="destructive"
              size="sm"
              class="mt-2.5 w-full"
              onClick={handleStop}
              disabled={loading}
            >
              {loading ? "Stopping…" : "Stop Timer"}
            </Button>
          </div>

          <div class="flex items-center justify-between px-4 py-1.5 border-t border-emerald-500/20">
            <span class="text-[10px] text-muted-foreground">
              {sessions.length} session{sessions.length !== 1 && "s"}
            </span>
            <span class="text-[11px] font-bold font-mono tabular-nums text-foreground/75">
              {formatDuration(totalHours)} today
            </span>
          </div>
        </Card>

        <SessionBlock tasks={groupedTasks} onResume={handleResume} loading={loading} />
      </Shell>
    );
  }

  /* ═══ STOPPED ═══ */
  return (
    <Shell
      user={user}
      onLogout={onLogout}
      dashboardURL={dashboardURL}
      bottom={
        <>
          <ErrorBar error={error} />
          <SessionBanner message={sessionBanner} onDismiss={dismissSessionBanner} />
          <div class="px-3 py-2.5 border-t border-border bg-card">
            <TaskSelector onStart={handleStart} loading={loading} />
          </div>
        </>
      }
    >
      <div class="flex items-center justify-between px-4 py-3 border-b border-border">
        <div>
          <p class="text-sm font-bold text-foreground">Time Tracker</p>
          {sessions.length > 0 && (
            <p class="text-[11px] text-muted-foreground">
              {sessions.length} session{sessions.length !== 1 && "s"} today
            </p>
          )}
        </div>
        <span class="text-xl font-bold font-mono tabular-nums text-foreground/80">
          {sessions.length > 0 ? formatDuration(totalHours) : "00:00:00"}
        </span>
      </div>

      <SessionBlock tasks={groupedTasks} onResume={handleResume} loading={loading} />
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
}: {
  user: User;
  onLogout: () => void;
  children: any;
  bottom?: any;
  dashboardURL?: string;
}) {
  const { isDark, toggle } = useTheme();
  const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(null);
  const [updating, setUpdating] = useState(false);

  // Listen for update:available event from Go backend. Clear any stale
  // handler first (C-FE-2 pattern — defensive against Preact
  // fast-refresh / double mount).
  useEffect(() => {
    window.runtime.EventsOff("update:available");
    window.runtime.EventsOn("update:available", (info: UpdateInfo) => {
      if (info?.available) setUpdateInfo(info);
    });
    return () => window.runtime.EventsOff("update:available");
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
    <div class="flex flex-col h-screen bg-background">
      <header class="flex items-center justify-between px-3 py-2.5 bg-card border-b border-border">
        <div class="flex items-center gap-2.5">
          {user.avatarUrl ? (
            <img src={user.avatarUrl} alt={user.name} class="w-7 h-7 rounded-md object-cover" />
          ) : (
            <div class="w-7 h-7 rounded-md bg-primary/10 flex items-center justify-center">
              <span class="text-[11px] font-bold text-primary">{user.name?.charAt(0) || "?"}</span>
            </div>
          )}
          <div class="min-w-0">
            <p class="text-[13px] font-semibold leading-tight text-foreground truncate">{user.name}</p>
            <p class="text-[10px] text-muted-foreground truncate">
              {user.employeeId && (
                <span class="font-medium text-primary">{user.employeeId}</span>
              )}
              {user.employeeId && " · "}
              {user.email}
            </p>
          </div>
        </div>
        <div class="flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon"
            class="h-7 w-7 text-muted-foreground"
            onClick={toggle}
            aria-label={isDark ? "Switch to light mode" : "Switch to dark mode"}
            title={isDark ? "Light mode" : "Dark mode"}
          >
            {isDark ? <SunIcon /> : <MoonIcon />}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            class="h-7 px-2 text-xs text-muted-foreground"
            onClick={onLogout}
          >
            Sign Out
          </Button>
        </div>
      </header>

      {updateInfo && (
        <div class="flex items-center justify-between px-3 py-2 bg-primary/10 border-b border-border">
          <p class="text-xs font-medium text-primary">v{updateInfo.version} available</p>
          <Button size="sm" class="h-7 px-3 text-xs" onClick={handleUpdate} disabled={updating}>
            {updating ? "Updating…" : "Update Now"}
          </Button>
        </div>
      )}

      <div class="flex-1 overflow-y-auto">{children}</div>

      {bottom}

      <footer class="px-3 py-1.5 flex items-center justify-between bg-card border-t border-border">
        <div class="flex items-center gap-1.5">
          <TaskFlowLogo size={16} />
          <span class="text-[10px] font-extrabold tracking-tight text-muted-foreground">
            Task<span class="text-primary">Flow</span>
          </span>
        </div>
        {dashboardURL && (
          <a
            href={dashboardURL}
            target="_blank"
            class="text-[10px] font-medium text-muted-foreground hover:text-foreground transition-colors"
          >
            Dashboard ↗
          </a>
        )}
      </footer>
    </div>
  );
}

/* ════════════════ Sessions ════════════════ */

function SessionBlock({ tasks, onResume, loading }: { tasks: GroupedTask[]; onResume: (t: GroupedTask) => void; loading: boolean }) {
  if (tasks.length === 0) return null;
  const total = tasks.reduce((s, t) => s + t.totalHours, 0);
  return (
    <Card class="mx-3 mt-3 overflow-hidden">
      <div class="px-3 py-2 bg-muted/50 border-b border-border">
        <span class="text-[10px] font-bold uppercase tracking-[0.1em] text-muted-foreground">
          Today's Sessions
        </span>
      </div>
      <div>
        {tasks.map((t, i) => (
          <TaskRow key={i} task={t} onResume={() => onResume(t)} loading={loading} />
        ))}
      </div>
      <div class="flex items-center justify-between px-3 py-2 bg-muted/50 border-t border-border">
        <span class="text-[10px] font-bold uppercase tracking-[0.1em] text-muted-foreground">Total</span>
        <span class="text-[13px] font-bold font-mono tabular-nums text-foreground">{formatDuration(total)}</span>
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

function TaskRow({ task, onResume, loading }: { task: GroupedTask; onResume: () => void; loading: boolean }) {
  return (
    <div class="flex items-center gap-2.5 px-3 py-2.5 border-b border-border last:border-0">
      <Button
        variant="ghost"
        size="icon"
        class="h-7 w-7 flex-shrink-0 bg-primary/10 text-primary hover:bg-primary/20 hover:scale-105 active:scale-95"
        onClick={onResume}
        disabled={loading}
        title="Resume"
        aria-label={`Resume ${task.taskTitle}`}
      >
        <svg class="w-3 h-3 ml-0.5" fill="currentColor" viewBox="0 0 24 24">
          <path d="M8 5v14l11-7z" />
        </svg>
      </Button>
      <div class="min-w-0 flex-1">
        <p class="text-xs font-medium truncate leading-tight text-foreground">{task.taskTitle}</p>
        <p class="text-[10px] truncate leading-tight text-muted-foreground">
          {task.projectName}
          {task.description && task.description !== task.taskTitle && (
            <span class="italic"> — {task.description}</span>
          )}
          <span class="opacity-60"> · {task.sessionCount}x</span>
        </p>
      </div>
      <span
        class={cn(
          "text-[13px] font-bold font-mono tabular-nums flex-shrink-0",
          task.isRunning ? "text-emerald-600 dark:text-emerald-400" : "text-primary",
        )}
      >
        {formatDuration(task.totalHours)}
      </span>
    </div>
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

/* ════════════════ Utils ════════════════ */

function getSessionHours(session: AttendanceSession): number {
  if (!session.signOutAt) return (Date.now() - new Date(session.signInAt).getTime()) / 3600000;
  if (session.hours && session.hours > 0) return session.hours;
  return (new Date(session.signOutAt).getTime() - new Date(session.signInAt).getTime()) / 3600000;
}

function groupSessionsByTask(sessions: AttendanceSession[]): GroupedTask[] {
  const map = new Map<string, GroupedTask>();
  for (const s of sessions) {
    const key = s.taskId || s.taskTitle || s.description || "general";
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
