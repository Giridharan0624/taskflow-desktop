import { useState, useEffect } from "preact/hooks";
import { LoginForm } from "./components/LoginForm";
import { TimerView } from "./components/TimerView";

// Wails runtime bindings (available at runtime via wails v2)
declare global {
  interface Window {
    go: {
      main: {
        App: {
          Login(email: string, password: string): Promise<LoginResult>;
          SetNewPassword(newPassword: string): Promise<void>;
          Logout(): Promise<void>;
          SignIn(data: StartTimerData): Promise<Attendance>;
          SignOut(): Promise<Attendance>;
          GetMyAttendance(): Promise<Attendance>;
          GetMyTasks(): Promise<Task[]>;
          GetCurrentUser(): Promise<User>;
          ShowWindow(): Promise<void>;
          CheckForUpdate(): Promise<UpdateInfo>;
          InstallUpdate(): Promise<void>;
          GetAppVersion(): Promise<string>;
          GetWebDashboardURL(): Promise<string>;
          GetSessionInfo(): Promise<SessionInfo>;
          /** Returns activity-monitor idle seconds. Polled from
           *  TimerView while a timer is active to drive the
           *  "still working?" prompt. */
          GetIdleSeconds(): Promise<number>;
          /** Persist the current window dimensions so the next launch
           *  restores the same size. Best-effort — failure is logged
           *  Go-side and never thrown to the caller. */
          SaveWindowSize(width: number, height: number): Promise<void>;
          /** Enable / disable launch-at-OS-login. Throws on OS write
           *  failure so the UI can revert the toggle. */
          SetAutoStart(enabled: boolean): Promise<void>;
          /** Read the current OS-level auto-launch state — used to
           *  hydrate the settings drawer toggle on open. */
          GetAutoStart(): Promise<boolean>;
          /** Wipe every queue + cache directory. Tokens stay in the
           *  OS keyring (cleared on Logout, not here). */
          ClearLocalCache(): Promise<void>;
          /** Surface a transient tray balloon. Frontend gates by the
           *  user's `notifications` setting before calling. */
          ShowTrayNotification(title: string, message: string): Promise<void>;
        };
      };
    };
    runtime: {
      EventsOn(event: string, callback: (...args: any[]) => void): void;
      EventsOff(event: string): void;
    };
  }
}

export interface LoginResult {
  success: boolean;
  requiresNewPassword: boolean;
  userId?: string;
  email?: string;
  name?: string;
}

export interface SessionInfo {
  platform: "windows" | "darwin" | "linux";
  sessionType: "x11" | "wayland" | "native" | "unknown";
  canTrackWindows: boolean;
  limitationMessage: string;
}

export interface StartTimerData {
  taskId: string;
  projectId: string;
  taskTitle: string;
  projectName: string;
  description: string;
}

export interface Attendance {
  userId: string;
  date: string;
  sessions: AttendanceSession[];
  totalHours: number;
  currentSignInAt: string | null;
  currentTask: CurrentTask | null;
  userName: string;
  userEmail: string;
  systemRole: string;
  status: "SIGNED_IN" | "SIGNED_OUT";
  sessionCount: number;
  /** UTC ISO timestamp captured by the backend when it built this
   *  response. The frontend feeds it into serverClock so the Timer
   *  ticks against server time, not the local OS clock — cross-device
   *  displays agree even when one device's clock is drifted.
   *  Optional: old backends pre the Phase-6 sync change don't emit it. */
  serverTime?: string;
}

export interface AttendanceSession {
  signInAt: string;
  signOutAt: string | null;
  hours: number | null;
  taskId: string | null;
  projectId: string | null;
  taskTitle: string | null;
  projectName: string | null;
  description: string | null;
}

export interface CurrentTask {
  taskId: string;
  projectId: string;
  taskTitle: string;
  projectName: string;
}

export interface Task {
  taskId: string;
  projectId: string;
  title: string;
  description: string | null;
  status: string;
  priority: string;
  domain: string;
  assignedTo: string[];
  deadline: string;
  projectName?: string;
}

export interface User {
  userId: string;
  email: string;
  name: string;
  systemRole: string;
  department: string | null;
  avatarUrl: string | null;
  employeeId: string | null;
  skills: string[];
}

export interface UpdateInfo {
  available: boolean;
  version: string;
  currentVersion: string;
  downloadUrl: string;
  releaseNotes: string;
  fileName: string;
  size: number;
}

export function App() {
  const [authenticated, setAuthenticated] = useState(false);
  const [loading, setLoading] = useState(true);
  const [user, setUser] = useState<User | null>(null);

  useEffect(() => {
    // Check if session was restored from keychain
    checkAuth();
  }, []);

  // Persist the window dimensions on resize, debounced. The handler
  // runs on every resize tick (Wails fires hundreds during a drag) so
  // the debounce keeps disk writes to one per "resize gesture" rather
  // than one per pixel. 400 ms feels responsive without thrashing IO.
  useEffect(() => {
    let t: ReturnType<typeof setTimeout> | null = null;
    function onResize() {
      if (t) clearTimeout(t);
      t = setTimeout(() => {
        const w = window.innerWidth;
        const h = window.innerHeight;
        // The Go binding clamps + sanity-checks; we don't bother
        // here, just fire-and-forget. If the IPC errors (e.g. during
        // shutdown) the catch swallows it.
        window.go.main.App.SaveWindowSize(w, h).catch(() => {});
      }, 400);
    }
    window.addEventListener("resize", onResize);
    return () => {
      window.removeEventListener("resize", onResize);
      if (t) clearTimeout(t);
    };
  }, []);

  async function checkAuth() {
    try {
      const currentUser = await window.go.main.App.GetCurrentUser();
      if (currentUser) {
        setUser(currentUser);
        setAuthenticated(true);
      }
    } catch {
      // Not authenticated, show login
    } finally {
      setLoading(false);
    }
  }

  function handleLoginSuccess(u: User) {
    setUser(u);
    setAuthenticated(true);
  }

  async function handleLogout() {
    await window.go.main.App.Logout();
    setUser(null);
    setAuthenticated(false);
  }

  if (loading) {
    return (
      <div class="flex items-center justify-center h-screen" style={{ background: "var(--surface-0)" }}>
        <div class="animate-spin rounded-full h-8 w-8 border-b-2" style={{ borderColor: "var(--accent)" }} />
      </div>
    );
  }

  if (!authenticated) {
    return <LoginForm onSuccess={handleLoginSuccess} />;
  }

  return <TimerView user={user!} onLogout={handleLogout} />;
}
