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
