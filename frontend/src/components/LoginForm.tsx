import { useState } from "preact/hooks"
import type { User, LoginResult } from "../app"
import { useTheme } from "../lib/useTheme"
import { TaskFlowLogo } from "./Logo"
import { friendlyError } from "../lib/errors"
import { Button } from "./ui/Button"
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "./ui/Card"
import { Input } from "./ui/Input"
import { Label } from "./ui/Label"

interface LoginFormProps {
  onSuccess: (user: User) => void
}

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
    <div class="flex flex-col h-screen bg-background">
      {/* Top bar with theme toggle */}
      <div class="flex items-center justify-end px-3 py-2">
        <Button
          variant="outline"
          size="icon"
          class="h-7 w-7 text-muted-foreground"
          onClick={toggle}
          aria-label={isDark ? "Switch to light mode" : "Switch to dark mode"}
        >
          {isDark ? <SunIcon /> : <MoonIcon />}
        </Button>
      </div>

      {/* Center content */}
      <div class="flex-1 flex flex-col items-center justify-center px-8 -mt-4">
        <div class="mb-6 text-center">
          <TaskFlowLogo size={48} class="mx-auto mb-3" />
          <h1 class="text-lg font-extrabold tracking-tight text-foreground">
            Task<span class="text-primary">Flow</span>
          </h1>
          <p class="text-[11px] mt-0.5 text-muted-foreground">Desktop Time Tracker</p>
        </div>

        <Card class="w-full max-w-sm">
          {challengePending ? (
            <>
              <CardHeader class="pb-3">
                <CardTitle>Set New Password</CardTitle>
                <CardDescription>First login — choose a password.</CardDescription>
              </CardHeader>
              <CardContent>
                <form onSubmit={handleNewPassword} class="space-y-4">
                  <Input
                    type="password"
                    placeholder="Min 8 chars, upper, lower, digit"
                    value={newPassword}
                    onInput={(e) => setNewPassword((e.target as HTMLInputElement).value)}
                    required
                    minLength={8}
                    autoFocus
                  />
                  {error && <ErrorBox msg={error} />}
                  <Button type="submit" class="w-full" disabled={loading}>
                    {loading ? "Setting…" : "Set Password & Continue"}
                  </Button>
                </form>
              </CardContent>
            </>
          ) : (
            <>
              <CardHeader class="pb-3">
                <CardTitle>Sign In</CardTitle>
              </CardHeader>
              <CardContent>
                <form onSubmit={handleLogin} class="space-y-3">
                  <div class="space-y-1.5">
                    <Label htmlFor="identifier">Email or Employee ID</Label>
                    <Input
                      id="identifier"
                      type="text"
                      placeholder="you@company.com or NS-26XXXX"
                      value={email}
                      onInput={(e) => setEmail((e.target as HTMLInputElement).value)}
                      required
                      autoFocus
                    />
                  </div>
                  <div class="space-y-1.5">
                    <Label htmlFor="password">Password</Label>
                    <Input
                      id="password"
                      type="password"
                      placeholder="Enter your password"
                      value={password}
                      onInput={(e) => setPassword((e.target as HTMLInputElement).value)}
                      required
                    />
                  </div>
                  {error && <ErrorBox msg={error} />}
                  <Button type="submit" class="w-full" disabled={loading}>
                    {loading ? "Signing in…" : "Sign In"}
                  </Button>
                </form>
              </CardContent>
            </>
          )}
        </Card>
      </div>

      {/* Footer */}
      <div class="py-2 text-center">
        <p class="text-[10px] text-muted-foreground">NeuroStack © 2026</p>
      </div>
    </div>
  )
}

function ErrorBox({ msg }: { msg: string }) {
  return (
    <div
      role="alert"
      class="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive"
    >
      {msg}
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
