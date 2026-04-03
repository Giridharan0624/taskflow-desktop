import { useState, useEffect, useMemo } from "preact/hooks";
import type {
  User,
  Attendance,
  AttendanceSession,
  StartTimerData,
} from "../app";
import { Timer, formatDuration } from "./Timer";
import { TaskSelector } from "./TaskSelector";
import { TaskFlowLogo } from "./Logo";
import { useTheme } from "../lib/useTheme";

interface TimerViewProps {
  user: User;
  onLogout: () => void;
}

// Module-level variable to persist optimistic timestamp across re-renders and polling
let _optimisticSignInAt: string | null = null;

export function TimerView({ user, onLogout }: TimerViewProps) {
  const [attendance, setAttendance] = useState<Attendance | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [, tick] = useState(0);
  const isActive = attendance?.status === "SIGNED_IN";

  useEffect(() => {
    if (!isActive) return;
    const i = setInterval(() => tick((t) => t + 1), 1000);
    return () => clearInterval(i);
  }, [isActive]);

  // Patch attendance with optimistic timestamp so polling doesn't reset the timer
  function patchAttendance(d: Attendance | null): Attendance | null {
    if (!d) { _optimisticSignInAt = null; return null; }
    if (_optimisticSignInAt && d.status === "SIGNED_IN") {
      d.currentSignInAt = _optimisticSignInAt;
      // Also patch the active session's signInAt
      const active = d.sessions?.find((s) => !s.signOutAt);
      if (active) active.signInAt = _optimisticSignInAt;
    }
    if (d.status !== "SIGNED_IN") _optimisticSignInAt = null;
    return d;
  }

  useEffect(() => {
    window.go.main.App.GetMyAttendance()
      .then((d: Attendance | null) => setAttendance(patchAttendance(d)))
      .catch(() => {});
    window.runtime.EventsOn("attendance:updated", (d: Attendance | null) =>
      setAttendance(patchAttendance(d ?? null))
    );
    return () => window.runtime.EventsOff("attendance:updated");
  }, []);

  const sessions = useMemo(() => {
    const raw = attendance?.sessions ?? [];
    if (isActive && attendance?.currentSignInAt)
      return raw.map((s) =>
        !s.signOutAt ? { ...s, signInAt: attendance.currentSignInAt! } : s
      );
    return raw;
  }, [attendance, isActive]);

  const totalHours = useMemo(
    () => sessions.reduce((sum, s) => sum + getSessionHours(s), 0),
    [sessions, isActive ? Date.now() : 0]
  );

  const groupedTasks = useMemo(
    () => groupSessionsByTask(sessions),
    [sessions, totalHours]
  );

  async function handleStart(data: StartTimerData) {
    setLoading(true);
    setError("");
    const t0 = new Date().toISOString();
    _optimisticSignInAt = t0; // Persist across polling updates
    try {
      const r = await window.go.main.App.SignIn(data);
      setAttendance(patchAttendance(r));
    } catch (err: any) {
      const msg = typeof err === "string" ? err : err?.message || "";
      if (msg.includes("already signed in")) {
        _optimisticSignInAt = null; // Don't override server time for existing session
        const c = await window.go.main.App.GetMyAttendance().catch(() => null);
        if (c) setAttendance(c);
      } else {
        _optimisticSignInAt = null;
        setError(msg || "Failed to start timer");
      }
    } finally {
      setLoading(false);
    }
  }

  async function handleStop() {
    setLoading(true);
    setError("");
    _optimisticSignInAt = null;
    try {
      setAttendance(await window.go.main.App.SignOut());
    } catch (err: any) {
      const msg = typeof err === "string" ? err : err?.message || "";
      setError(msg || "Failed to stop timer");
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
      <Shell user={user} onLogout={onLogout}
        bottom={
          <>
            <ErrorBar error={error} />
            <div class="px-3 py-2.5" style={{ borderTop: "1px solid var(--color-border)", background: "var(--color-surface)" }}>
              <p class="text-[10px] font-bold uppercase tracking-widest mb-1.5" style={{ color: "var(--color-text-muted)" }}>
                Switch Task
              </p>
              <TaskSelector onStart={handleStart} loading={loading} />
            </div>
          </>
        }
      >
        {/* Live timer card */}
        <div class="mx-3 mt-3 card overflow-hidden" style={{ borderColor: "var(--color-live-border)", background: "var(--color-live-bg)" }}>
          <div class="px-4 pt-3 pb-2.5 text-center">
            {/* Status */}
            <div class="flex items-center justify-center gap-2 mb-1.5">
              <span class="relative flex h-2 w-2">
                <span class="animate-ping absolute h-full w-full rounded-full opacity-60" style={{ background: "var(--color-live)" }} />
                <span class="relative rounded-full h-2 w-2" style={{ background: "var(--color-live)" }} />
              </span>
              <span class="text-[10px] font-semibold uppercase tracking-widest" style={{ color: "var(--color-live-text)" }}>
                Recording
              </span>
            </div>

            {/* Timer */}
            {attendance.currentSignInAt && (
              <Timer startTime={attendance.currentSignInAt} class="timer-display text-[34px]" />
            )}

            {/* Task info */}
            <p class="text-[12px] font-semibold mt-1.5 truncate" style={{ color: "var(--color-text)" }}>
              {cur?.taskTitle || "Working"}
            </p>
            <p class="text-[10px] truncate" style={{ color: "var(--color-text-muted)" }}>
              {cur?.projectName}
              {curSess?.description && <span class="italic"> — {curSess.description}</span>}
            </p>

            {/* Stop */}
            <button class="btn-stop mt-2.5 w-full" onClick={handleStop} disabled={loading}>
              {loading ? "Stopping..." : "Stop Timer"}
            </button>
          </div>

          {/* Stats bar */}
          <div class="flex items-center justify-between px-4 py-1.5" style={{ borderTop: "1px solid var(--color-live-border)" }}>
            <span class="text-[10px]" style={{ color: "var(--color-text-muted)" }}>
              {sessions.length} session{sessions.length !== 1 && "s"}
            </span>
            <span class="text-[11px] font-bold font-mono tabular-nums" style={{ color: "var(--color-text-secondary)" }}>
              {formatDuration(totalHours)} today
            </span>
          </div>
        </div>

        {/* Sessions */}
        <SessionBlock tasks={groupedTasks} onResume={handleResume} loading={loading} />
      </Shell>
    );
  }

  /* ═══ STOPPED ═══ */
  return (
    <Shell user={user} onLogout={onLogout}
      bottom={
        <>
          <ErrorBar error={error} />
          <div class="px-3 py-2.5" style={{ borderTop: "1px solid var(--color-border)", background: "var(--color-surface)" }}>
            <TaskSelector onStart={handleStart} loading={loading} />
          </div>
        </>
      }
    >
      {/* Header bar */}
      <div class="flex items-center justify-between px-4 py-3" style={{ borderBottom: "1px solid var(--color-border)" }}>
        <div>
          <p class="text-[14px] font-bold" style={{ color: "var(--color-text)" }}>Time Tracker</p>
          {sessions.length > 0 && (
            <p class="text-[11px]" style={{ color: "var(--color-text-muted)" }}>
              {sessions.length} session{sessions.length !== 1 && "s"} today
            </p>
          )}
        </div>
        <span class="text-[20px] font-bold font-mono tabular-nums" style={{ color: "var(--color-text-secondary)" }}>
          {sessions.length > 0 ? formatDuration(totalHours) : "00:00:00"}
        </span>
      </div>

      {/* Sessions */}
      <SessionBlock tasks={groupedTasks} onResume={handleResume} loading={loading} />
    </Shell>
  );
}

/* ════════════════ Shell ════════════════ */

function Shell({ user, onLogout, children, bottom }: { user: User; onLogout: () => void; children: any; bottom?: any }) {
  const { isDark, toggle } = useTheme();
  return (
    <div class="flex flex-col h-screen" style={{ background: "var(--color-bg)" }}>
      {/* Header */}
      <header class="flex items-center justify-between px-3 py-2.5" style={{ background: "var(--color-surface)", borderBottom: "1px solid var(--color-border)" }}>
        <div class="flex items-center gap-2.5">
          {user.avatarUrl ? (
            <img src={user.avatarUrl} alt={user.name} class="w-7 h-7 rounded-xl object-cover" />
          ) : (
            <div class="w-7 h-7 rounded-xl flex items-center justify-center" style={{ background: "var(--color-primary-light)" }}>
              <span class="text-[11px] font-bold" style={{ color: "var(--color-primary)" }}>{user.name?.charAt(0) || "?"}</span>
            </div>
          )}
          <div>
            <p class="text-[13px] font-semibold leading-tight" style={{ color: "var(--color-text)" }}>{user.name}</p>
            <p class="text-[10px]" style={{ color: "var(--color-text-muted)" }}>
              {user.employeeId && <span class="font-medium" style={{ color: "var(--color-primary)" }}>{user.employeeId}</span>}
              {user.employeeId && " · "}
              {user.email}
            </p>
          </div>
        </div>
        <div class="flex items-center gap-1.5">
          <button
            onClick={toggle}
            class="w-7 h-7 rounded-lg flex items-center justify-center transition-colors"
            style={{ background: "var(--color-surface-hover)", color: "var(--color-text-muted)" }}
            title={isDark ? "Light mode" : "Dark mode"}
          >
            {isDark ? <SunIcon /> : <MoonIcon />}
          </button>
          <button class="btn-ghost px-2 py-1" onClick={onLogout}>Sign Out</button>
        </div>
      </header>

      <div class="flex-1 overflow-y-auto">{children}</div>

      {bottom}

      <footer class="px-3 py-1.5 flex items-center justify-between" style={{ background: "var(--color-surface)", borderTop: "1px solid var(--color-border)" }}>
        <div class="flex items-center gap-1.5">
          <TaskFlowLogo size={16} />
          <span class="text-[10px] font-extrabold tracking-tight" style={{ color: "var(--color-text-muted)" }}>
            Task<span style={{ color: "var(--color-primary)" }}>Flow</span>
          </span>
        </div>
        <a href="https://taskflow-ns.vercel.app" target="_blank"
          class="text-[10px] font-medium transition-colors" style={{ color: "var(--color-text-muted)" }}>
          Dashboard ↗
        </a>
      </footer>
    </div>
  );
}

/* ════════════════ Sessions ════════════════ */

function SessionBlock({ tasks, onResume, loading }: { tasks: GroupedTask[]; onResume: (t: GroupedTask) => void; loading: boolean }) {
  if (tasks.length === 0) return null;
  const total = tasks.reduce((s, t) => s + t.totalHours, 0);
  return (
    <div class="mx-3 mt-3 card overflow-hidden">
      <div class="px-3 py-2" style={{ background: "var(--color-surface-hover)", borderBottom: "1px solid var(--color-border-light)" }}>
        <span class="text-[10px] font-bold uppercase tracking-[0.1em]" style={{ color: "var(--color-text-muted)" }}>
          Today's Sessions
        </span>
      </div>
      <div>
        {tasks.map((t, i) => (
          <TaskRow key={i} task={t} onResume={() => onResume(t)} loading={loading} />
        ))}
      </div>
      <div class="flex items-center justify-between px-3 py-2" style={{ background: "var(--color-surface-hover)", borderTop: "1px solid var(--color-border-light)" }}>
        <span class="text-[10px] font-bold uppercase tracking-[0.1em]" style={{ color: "var(--color-text-muted)" }}>Total</span>
        <span class="text-[13px] font-bold font-mono tabular-nums" style={{ color: "var(--color-text)" }}>{formatDuration(total)}</span>
      </div>
    </div>
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
    <div class="flex items-center gap-2.5 px-3 py-2.5 transition-colors" style={{ borderBottom: "1px solid var(--color-border-light)" }}>
      <button
        onClick={onResume}
        disabled={loading}
        class="flex-shrink-0 w-7 h-7 rounded-lg flex items-center justify-center transition-all disabled:opacity-20 hover:scale-105 active:scale-95"
        style={{ background: "var(--color-primary-light)", color: "var(--color-primary)" }}
        title="Resume"
      >
        <svg class="w-3 h-3 ml-0.5" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z" /></svg>
      </button>
      <div class="min-w-0 flex-1">
        <p class="text-[12px] font-medium truncate leading-tight" style={{ color: "var(--color-text)" }}>{task.taskTitle}</p>
        <p class="text-[10px] truncate leading-tight" style={{ color: "var(--color-text-muted)" }}>
          {task.projectName}
          {task.description && task.description !== task.taskTitle && <span class="italic"> — {task.description}</span>}
          <span class="opacity-60"> · {task.sessionCount}x</span>
        </p>
      </div>
      <span class={`text-[13px] font-bold font-mono tabular-nums flex-shrink-0`}
        style={{ color: task.isRunning ? "var(--color-live-text)" : "var(--color-primary)" }}>
        {formatDuration(task.totalHours)}
      </span>
    </div>
  );
}

function ErrorBar({ error }: { error: string }) {
  if (!error) return null;
  return (
    <div class="mx-3 mb-2 text-[11px] p-2 rounded-xl" style={{ background: "var(--color-danger-bg)", border: "1px solid var(--color-danger-border)", color: "var(--color-danger)" }}>
      {error}
    </div>
  );
}

/* ════════════════ Icons ════════════════ */

function SunIcon() {
  return (
    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
      <circle cx="12" cy="12" r="5" /><path d="M12 1v2M12 21v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M1 12h2M21 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42" />
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
