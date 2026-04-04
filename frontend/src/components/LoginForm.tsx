import { useState } from "preact/hooks";
import type { User, LoginResult } from "../app";
import { useTheme } from "../lib/useTheme";
import { TaskFlowLogo } from "./Logo";
import { friendlyError } from "../lib/errors";

interface LoginFormProps {
  onSuccess: (user: User) => void;
}

export function LoginForm({ onSuccess }: LoginFormProps) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [challengeSession, setChallengeSession] = useState<string | null>(null);
  const { isDark, toggle } = useTheme();

  async function handleLogin(e: Event) {
    e.preventDefault();
    setError("");
    setLoading(true);
    try {
      const result: LoginResult = await window.go.main.App.Login(email, password);
      if (result.requiresNewPassword) { setChallengeSession(result.session!); setLoading(false); return; }
      onSuccess(await window.go.main.App.GetCurrentUser());
    } catch (err: any) {
      setError(friendlyError(err));
    } finally { setLoading(false); }
  }

  async function handleNewPassword(e: Event) {
    e.preventDefault();
    setError("");
    setLoading(true);
    try {
      await window.go.main.App.SetNewPassword(challengeSession!, newPassword);
      onSuccess(await window.go.main.App.GetCurrentUser());
    } catch (err: any) {
      setError(friendlyError(err));
    } finally { setLoading(false); }
  }

  return (
    <div class="flex flex-col items-center justify-center h-screen px-8 relative" style={{ background: "var(--color-bg)" }}>
      {/* Theme toggle */}
      <button onClick={toggle}
        class="absolute top-3 right-3 w-8 h-8 rounded-xl flex items-center justify-center transition-colors"
        style={{ background: "var(--color-surface)", color: "var(--color-text-muted)", border: "1px solid var(--color-border)" }}>
        {isDark ? (
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <circle cx="12" cy="12" r="5" /><path d="M12 1v2M12 21v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M1 12h2M21 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42" />
          </svg>
        ) : (
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path d="M21 12.79A9 9 0 1111.21 3 7 7 0 0021 12.79z" />
          </svg>
        )}
      </button>

      {/* Logo */}
      <div class="mb-8 text-center">
        <TaskFlowLogo size={56} class="mx-auto mb-4" />
        <h1 class="text-xl font-extrabold tracking-tight" style={{ color: "var(--color-text)" }}>
          Task<span style={{ color: "var(--color-primary)" }}>Flow</span>
        </h1>
        <p class="text-[12px] mt-1" style={{ color: "var(--color-text-muted)" }}>Desktop Time Tracker</p>
      </div>

      <div class="w-full max-w-sm card p-5">
        {challengeSession ? (
          <form onSubmit={handleNewPassword}>
            <h2 class="text-[15px] font-semibold mb-1" style={{ color: "var(--color-text)" }}>Set New Password</h2>
            <p class="text-[11px] mb-4" style={{ color: "var(--color-text-muted)" }}>First login — choose a password.</p>
            <input type="password" class="input mb-4" placeholder="Min 8 chars, upper, lower, digit"
              value={newPassword} onInput={(e) => setNewPassword((e.target as HTMLInputElement).value)}
              required minLength={8} autoFocus />
            {error && <ErrorBox msg={error} />}
            <button type="submit" class="btn-primary w-full" disabled={loading}>
              {loading ? "Setting..." : "Set Password & Continue"}
            </button>
          </form>
        ) : (
          <form onSubmit={handleLogin}>
            <h2 class="text-[15px] font-semibold mb-4" style={{ color: "var(--color-text)" }}>Sign In</h2>
            <div class="mb-3">
              <label class="block text-[11px] font-medium mb-1" style={{ color: "var(--color-text-secondary)" }}>
                Email or Employee ID
              </label>
              <input type="text" class="input" placeholder="you@company.com or NS-26XXXX"
                value={email} onInput={(e) => setEmail((e.target as HTMLInputElement).value)} required autoFocus />
            </div>
            <div class="mb-4">
              <label class="block text-[11px] font-medium mb-1" style={{ color: "var(--color-text-secondary)" }}>Password</label>
              <input type="password" class="input" placeholder="Enter your password"
                value={password} onInput={(e) => setPassword((e.target as HTMLInputElement).value)} required />
            </div>
            {error && <ErrorBox msg={error} />}
            <button type="submit" class="btn-primary w-full" disabled={loading}>
              {loading ? "Signing in..." : "Sign In"}
            </button>
          </form>
        )}
      </div>

      <p class="text-[10px] mt-6" style={{ color: "var(--color-text-muted)" }}>NeuroStack © 2026</p>
    </div>
  );
}

function ErrorBox({ msg }: { msg: string }) {
  return (
    <div class="text-[11px] mb-3 p-2 rounded-xl"
      style={{ background: "var(--color-danger-bg)", border: "1px solid var(--color-danger-border)", color: "var(--color-danger)" }}>
      {msg}
    </div>
  );
}
