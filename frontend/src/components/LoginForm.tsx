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
  // The Cognito challenge session lives entirely on the Go side; the
  // frontend only tracks whether a challenge is pending.
  const [challengePending, setChallengePending] = useState(false);
  const { isDark, toggle } = useTheme();

  async function handleLogin(e: Event) {
    e.preventDefault();
    setError("");
    setLoading(true);
    try {
      const result: LoginResult = await window.go.main.App.Login(email, password);
      // Clear the password out of state immediately after the Cognito
      // call returns — both on the happy path AND on the new-password
      // challenge branch. The previous implementation only cleared on
      // success, leaving the plaintext in React state for the entire
      // challenge flow; a component unmount mid-challenge would keep
      // the string retained until GC. See H-FE-2.
      setPassword("");
      if (result.requiresNewPassword) { setChallengePending(true); setLoading(false); return; }
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
      await window.go.main.App.SetNewPassword(newPassword);
      // Clear both password fields out of state after the challenge
      // completes. See H-FE-2.
      setPassword("");
      setNewPassword("");
      onSuccess(await window.go.main.App.GetCurrentUser());
    } catch (err: any) {
      setError(friendlyError(err));
    } finally { setLoading(false); }
  }

  return (
    <div class="flex flex-col h-screen" style={{ background: "var(--color-bg)" }}>
      {/* Top bar with theme toggle */}
      <div class="flex items-center justify-end px-3 py-2">
        <button onClick={toggle}
          class="w-7 h-7 rounded-lg flex items-center justify-center transition-colors"
          style={{ background: "var(--color-surface)", color: "var(--color-text-muted)", border: "1px solid var(--color-border)" }}>
          {isDark ? (
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <circle cx="12" cy="12" r="5" /><path d="M12 1v2M12 21v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M1 12h2M21 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42" />
            </svg>
          ) : (
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path d="M21 12.79A9 9 0 1111.21 3 7 7 0 0021 12.79z" />
            </svg>
          )}
        </button>
      </div>

      {/* Center content */}
      <div class="flex-1 flex flex-col items-center justify-center px-8 -mt-4">
        {/* Logo */}
        <div class="mb-6 text-center">
          <TaskFlowLogo size={48} class="mx-auto mb-3" />
          <h1 class="text-lg font-extrabold tracking-tight" style={{ color: "var(--color-text)" }}>
            Task<span style={{ color: "var(--color-primary)" }}>Flow</span>
          </h1>
          <p class="text-[11px] mt-0.5" style={{ color: "var(--color-text-muted)" }}>Desktop Time Tracker</p>
        </div>

        <div class="w-full max-w-sm card p-5">
        {challengePending ? (
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

      </div>

      {/* Footer */}
      <div class="py-2 text-center">
        <p class="text-[10px]" style={{ color: "var(--color-text-muted)" }}>NeuroStack © 2026</p>
      </div>
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
