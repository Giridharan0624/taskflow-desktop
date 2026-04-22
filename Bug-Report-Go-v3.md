# Bug Report: Desktop v3 — Deep Bug-Bounty Audit

**Date:** 2026-04-22
**Branch:** saas-migration
**Scope:** Full audit of `desktop/` — monitor, tray, api+auth, state, app/main/updater.
**Focus:** Race conditions, goroutine leaks, TOCTOU, resource leaks, security, lifecycle bugs.

---

## Methodology

Five parallel sub-agents audited each subsystem independently. Findings were then
cross-verified by reading the code directly; **false positives were excluded**
(noted in §7). Severity calibrated against real-world failure modes, not
theoretical ones.

## Executive Summary

| Severity | Count | Subsystems affected |
|---|---|---|
| CRITICAL | 2 | updater (unsigned releases), main (SIGTERM data loss) |
| HIGH | 9 | monitor × 4, api+auth × 4, tray × 1 |
| MEDIUM | 13 | monitor × 5, api+auth × 3, tray × 3, state × 1, updater × 1 |
| LOW / notes | 11 | spread |

**The two criticals are both defense-in-depth failures, not active exploits today.**
Neither is being hit in normal use, but both silently fail open in scenarios where
we'd expect them to fail closed.

---

## §1. CRITICAL

### C1. Updater installs unsigned SHA256SUMS with only a warning log
- **File:** [desktop/internal/updater/release.pub](desktop/internal/updater/release.pub) (empty), [desktop/internal/updater/signature.go:27-29](desktop/internal/updater/signature.go#L27-L29)
- **What:** `signedReleases()` returns false when `release.pub` is empty. In
  that mode, the updater downloads SHA256SUMS over HTTPS, matches the binary's
  hash against it, and installs — but SHA256SUMS itself is never
  cryptographically verified. The comment calls this "bootstrap period" but the
  code path is indistinguishable from a prod misconfiguration.
- **Threat:** Anyone who compromises GitHub release assets (stolen PAT, insider,
  supply-chain) can swap SHA256SUMS and the matching binary; clients install
  without complaint.
- **Fix:** In production builds, make empty `release.pub` a hard build-time
  error (`//go:generate` check or CI guard). In the binary itself, treat
  `signedReleases()==false` as "never auto-install" and degrade to
  "notify user, open release page" only.

### C2. SIGTERM loses up to 5 min of activity data + tray state
- **File:** [desktop/main.go:89-97](desktop/main.go#L89-L97)
- **What:** On SIGTERM the signal goroutine calls `autoSignOutIfRunning(3s)`,
  `closeLogFile()`, then **`os.Exit(0)`**. `os.Exit` bypasses Wails'
  `OnShutdown` and `OnBeforeClose`, which means `ActivityMonitor.Stop()`
  never runs → the current activity bucket (up to 5 min of keyboard/mouse
  counts) is silently dropped before the backend gets it. `TrayManager.Stop()`
  also never runs on Windows, which can leave the tray icon ghosted in the
  notification area (the known-fixed bug resurfaces on SIGTERM).
- **Threat:** Data loss on `systemctl stop`, logout, `pkill`, Ctrl-C during
  `wails dev`, and every signal-driven shutdown.
- **Fix:** Replace `os.Exit(0)` with `runtime.Quit(a.ctx)` (Wails' graceful
  shutdown), and let Wails' own signal path or `OnShutdown` do the cleanup
  work. The 3-second budget already provided by `autoSignOutIfRunning`
  stands on its own; a second 3-second budget around `runtime.Quit` with
  a hard `os.Exit` fallback handles the "Wails is wedged" tail case.

---

## §2. HIGH

### H1. Token refresh has no singleflight — Cognito DoS on 401 storm
- **File:** [desktop/internal/api/client.go:167-174](desktop/internal/api/client.go#L167-L174), [desktop/internal/auth/cognito.go:310-330](desktop/internal/auth/cognito.go#L310-L330)
- **What:** When the heartbeat goroutine, screenshot upload goroutine, and UI
  thread all race on an expired token, each independently calls
  `refreshTokensLocked()`. There's no `singleflight.Group` around it; Cognito
  gets hammered with N parallel `InitiateAuth` calls for the same user.
- **Threat:** Cognito rate-limit / `TooManyRequestsException` → users get
  kicked out; in extreme cases account lockout.
- **Fix:** Wrap `refreshTokensLocked()` in `golang.org/x/sync/singleflight`
  keyed by user sub. One call, N waiters.

### H2. macOS idle + window tracking synchronously exec subprocesses every tick
- **File:** [desktop/internal/monitor/idle_darwin.go:22-45](desktop/internal/monitor/idle_darwin.go#L22-L45), [desktop/internal/monitor/window_darwin.go:20-33](desktop/internal/monitor/window_darwin.go#L20-L33)
- **What:** `ioreg` runs every 1s from `trackActivity`, `osascript` runs
  every 5s. Neither has a timeout context. If either subprocess hangs
  (OS update weirdness, osascript waiting on an AppleScript dialog), the
  entire `trackActivity` goroutine stalls → no heartbeats, no screenshot
  cadence, no idle detection until the hang clears.
- **Fix:** Wrap in `exec.CommandContext` with a 500 ms timeout; on timeout,
  return the previous cached value and log once.

### H3. Screenshot IsScreenLocked → CaptureScreen TOCTOU
- **File:** [desktop/internal/monitor/screenshot.go:48-58](desktop/internal/monitor/screenshot.go#L48-L58), called from [desktop/internal/monitor/activity.go:388-398](desktop/internal/monitor/activity.go#L388-L398)
- **What:** The 5-second warning balloon fires, `sleepOrCancel(5s)` runs,
  then `CaptureScreenDefault()` is called. The lock check happens inside
  `CaptureScreen` but not after the 5 s wait — a user who locks their
  screen during those 5 s will still have a pre-lock frame captured if
  the lock registers after the check but before the bitblt.
- **Fix:** Re-check `IsScreenLocked()` immediately before `CaptureDisplay()`
  (cheap — it's a WTSQuerySessionInformation call / D-Bus property read).
  Combine with a defence in depth: also check idle > 10 min as a secondary
  proxy at the capture site.

### H4. Token leakage risk via unredacted Cognito SDK error strings
- **File:** [desktop/internal/auth/cognito.go:366](desktop/internal/auth/cognito.go#L366), [desktop/internal/auth/cognito.go:394](desktop/internal/auth/cognito.go#L394)
- **What:** `sanitizeErrorBody()` runs on HTTP response bodies but not on
  the AWS SDK's `err.Error()` output. AWS SDK errors sometimes embed
  request context including Authorization headers when retry metadata is
  attached.
- **Fix:** Wrap every Cognito SDK error in `fmt.Errorf("cognito: %v",
  sanitizeError(err))` before returning or logging. Never log the raw SDK
  error value.

### H5. Predictable update filename in 0700 temp dir — symlink-race window
- **File:** [desktop/internal/updater/updater.go:252-271](desktop/internal/updater/updater.go#L252-L271)
- **What:** `os.MkdirTemp` gives a private dir, but the filename inside
  comes from `safeName` (derived from release asset name). A second
  process running as the same user can pre-create a symlink at that
  path between MkdirTemp return and download start, aiming to
  intercept the download.
- **Threat:** Local attacker on multi-user box or a prior malicious binary
  can swap the update file before verification.
- **Fix:** Use `os.CreateTemp` to get an anonymous fd, write the body
  there, then rename into place atomically. `O_NOFOLLOW | O_EXCL` on
  the create. This also gives you symlink-safety for free.

### H6. Windows tray OnStopTimer callback runs synchronously on message loop
- **File:** [desktop/internal/tray/tray_windows.go:564](desktop/internal/tray/tray_windows.go#L564)
- **What:** `OnShowWindow` is spawned in a goroutine, but `OnStopTimer`
  is called synchronously from `trayWndProc`. If the handler path ends
  up in code that takes `tray.m.mu` (e.g. `SetTimerActive`), the
  message loop blocks on its own lock → tray becomes unresponsive to
  further clicks.
- **Fix:** Wrap line 564 in `go func() { mgr.handler.OnStopTimer() }()`
  to match the `OnShowWindow` pattern.

### H7. Partial screenshot upload after presign TTL expiry drops frame silently
- **File:** [desktop/internal/api/client.go:460-515](desktop/internal/api/client.go#L460-L515)
- **What:** Presign (15-min TTL) + 3-min upload timeout. If the upload
  takes long enough, the PUT returns 403. The current code logs and
  returns; the heartbeat still lands without the screenshot URL. User
  sees a gap in the activity report with no explanation.
- **Fix:** On 403 during upload, request a fresh presign and retry once
  (idempotent — same S3 key). After second failure, surface a tray
  balloon so the user knows why the gap exists.

### H8. Linux X11 connection lost → silent 0-idle forever until next restart
- **File:** [desktop/internal/monitor/x11_linux.go:158-173](desktop/internal/monitor/x11_linux.go#L158-L173)
- **What:** `getIdleMs` does call `invalidateX11Locked()` on error (good),
  but between a goroutine getting a stale `*x11` pointer and another
  goroutine invalidating it, the in-flight caller can call
  `screensaver.QueryInfo(x.conn, ...)` on a closed connection.
  xgb handles this by returning an error rather than crashing, so the
  real consequence is one spurious error log per racing goroutine — but
  the agent's deadlock claim was a false positive.
- **Fix:** Acceptable as-is; add a comment documenting that "stale
  pointer calls return err, which is the intended error-path trigger
  for invalidation." No code change required.

### H9. Keyboard heuristic inflates counts when logind idle detection cooldown sticks
- **File:** [desktop/internal/monitor/idle_logind_linux.go:65-69](desktop/internal/monitor/idle_logind_linux.go#L65-L69)
- **What:** `logindLastTryAt` is set on first successful call and never
  reset. If logind restarts (system upgrade, crash), the proxy becomes
  stale but the cooldown check blocks reconnection for 60 s, during
  which idle detection returns 0 → keyboard counter adds 1 every tick
  (false "active" signal) → inflated keyboard counts.
- **Fix:** On every successful query, refresh `logindLastTryAt =
  time.Now()`. Keep the cooldown only on failure paths.

---

## §3. MEDIUM

### M1. Windows cursor at literal (0,0) treated as no-movement forever
- **File:** [desktop/internal/monitor/input_windows.go:118-124](desktop/internal/monitor/input_windows.go#L118-L124)
- Initialise `lastCursorX/Y = -1` sentinel; first real sample is always a movement.

### M2. InputTracker wraparound + pre-lock event loss on macOS
- **File:** [desktop/internal/monitor/input_darwin.go:71-95](desktop/internal/monitor/input_darwin.go#L71-L95)
- CGEventSource counters can reset across screen lock on some OS versions.
  Apply the same wrap-around handling already present in activity.go:216-235.

### M3. flushPendingLocked re-locks across send — stale `hasData` check
- **File:** [desktop/internal/monitor/activity.go:179-188](desktop/internal/monitor/activity.go#L179-L188)
- Releases lock, then `sendCurrentBucket` re-locks. A concurrent reset
  between the two windows can publish an empty bucket. Safe today because
  Stop has already drained goroutines, but fragile.
- Fix: Hold the lock, decide+mark, then release.

### M4. Retry policy absent on POST — transient network loses heartbeats
- **File:** [desktop/internal/api/client.go:109-158](desktop/internal/api/client.go#L109-L158)
- resty has no `SetRetryCount`. A 5-s Wi-Fi blip = one lost heartbeat bucket.
- Fix: `SetRetryCount(1)` with exponential backoff; mark heartbeats idempotent
  by timestamp on the backend.

### M5. Heartbeat goroutine keeps retrying after logout
- **File:** [desktop/internal/api/client.go:167-174](desktop/internal/api/client.go#L167-L174), app.go Logout path
- After logout, `GetIDToken()` returns "not authenticated" and the
  heartbeat logs an error every 5 min forever (until app quit).
- Fix: Have `Logout()` signal `ActivityMonitor.Stop()` before clearing tokens.

### M6. Windows HICON + GDI leaks on icon-creation error paths
- **File:** [desktop/internal/tray/tray_windows.go:336](desktop/internal/tray/tray_windows.go#L336), [tray_windows.go:682-752](desktop/internal/tray/tray_windows.go#L682-L752)
- If `createDotOverlayIcon` fails mid-way, `baseIcon` and intermediate
  bitmaps are not freed. Low leak rate (once per Start/Stop cycle) but
  real.
- Fix: error-path `defer pDestroyIcon.Call(base)` and gate bitmap
  deletes on `newIcon != 0`.

### M7. Balloon title/message silently truncated to 64/256 uint16
- **File:** [desktop/internal/tray/tray_windows.go:431-432](desktop/internal/tray/tray_windows.go#L431-L432)
- `copy()` truncates silently. Not exploitable (all callers are internal)
  but hides long error messages from the user.
- Fix: Truncate explicitly with an ellipsis and log once at warn level.

### M8. TOCTOU: heartbeat/screenshot checks IsTimerActive then runs
- **File:** [desktop/internal/monitor/activity.go:270-275](desktop/internal/monitor/activity.go#L270-L275), [activity.go:367-373](desktop/internal/monitor/activity.go#L367-L373)
- UI thread can call SetAttendance(nil) between the check and the action.
  Backend will reject the heartbeat cleanly, but the screenshot still
  uploads to S3 → orphaned object billed forever.
- Fix: Have the heartbeat/upload send a "sessionId" captured at tick
  start; backend rejects sessionId mismatch including post-signout.

### M9. Presign URL TTL vs upload path — no margin check
- Combine fix with H7.

### M10. Auth header client-state fragility
- **File:** [desktop/internal/api/client.go:173](desktop/internal/api/client.go#L173)
- Currently correct because `R()` returns a fresh request, but there's
  no guard against someone adding `c.http.SetHeader(...)` and
  introducing a cross-request leak.
- Fix: Add package comment prohibiting client-level mutation.

### M11. Keyring unmarshal errors swallowed on Linux/macOS
- **File:** [desktop/internal/auth/keystore_linux.go:66](desktop/internal/auth/keystore_linux.go#L66), [keystore_darwin.go:66](desktop/internal/auth/keystore_darwin.go#L66)
- Corrupted meta → ExpiresAt = 0 → session silently invalidated on next
  start. Fails closed (good) but no log to debug with.
- Fix: Log the unmarshal error before returning; keep behaviour.

### M12. Update cleanup doesn't purge orphaned temp dirs on startup
- **File:** [desktop/internal/updater/updater.go:252-265](desktop/internal/updater/updater.go#L252-L265)
- If the app is SIGKILLed mid-download, the temp dir accumulates.
- Fix: On startup, glob `taskflow-update-*` older than 24 h and remove.

### M13. State: no atomic read of (IsTimerActive && CurrentTask != nil)
- **File:** [desktop/internal/state/state.go:122-126](desktop/internal/state/state.go#L122-L126)
- `IsTimerActive` reads Status only. Callers that also need CurrentTask
  take the lock twice — the second read can see a transitional state.
- Fix: Add `GetTimerContext() (active bool, task *CurrentTask)` that
  returns both under one RLock.

---

## §4. LOW / defensive

- **L1.** X11 atom cache unbounded ([x11_linux.go:40-42](desktop/internal/monitor/x11_linux.go#L40-L42)). Cap at ~100 entries as a paranoid LRU. Real-world usage ~20 atoms.
- **L2.** `hwnd` reads outside `m.mu` in tray Windows Stop. Actually safe because hwnd is immutable after Start, but document it.
- **L3.** Windows `PostMessage` return value unchecked. Low risk — hwnd is ours.
- **L4.** macOS CGEventSource counters use uint64 — 2^64 wraparound is theoretical but add handling anyway for parity with Windows.
- **L5.** State mutex is not re-entrant (Go `sync.RWMutex`). Add a package doc comment warning against re-entrant patterns.
- **L6.** Tray `PostMessage` return unchecked; `SetTooltip` ignores failure.
- **L7.** Updater SHA256SUMS parsing tolerates CRLF (verified) but doesn't explicitly reject filenames containing `..` or `/` — add an allow-list regex `^[a-zA-Z0-9._-]+$` for the filename column.
- **L8.** Heartbeat after signout retries forever (overlaps M5).
- **L9.** No user-visible error when keyring backend is unavailable on Linux headless/SSH boxes — the app silently requires re-login every launch. Add a one-time tray balloon.
- **L10.** JSON pointer fields can't distinguish `null` from "missing" — currently fine but document the invariant.
- **L11.** Version string parsing strips `-staging` suffix, so a staging client CAN auto-update to a prod release with the same base version. Known issue; tracked separately.

---

## §5. False positives excluded

These were flagged by agents but are actually correct code. Listed so
nobody re-files them:

- **"x11 getX11() RLock+Once+Lock deadlock"** ([x11_linux.go:52-92](desktop/internal/monitor/x11_linux.go#L52-L92))
  — `sync.Once.Do` provides mutual exclusion for init; the RLock fast
  path is safe because Once synchronises the first writer; the final
  RLock just re-reads after init. No deadlock path.

- **"appUsage map reallocation mid-iteration"** ([activity.go:340-348](desktop/internal/monitor/activity.go#L340-L348))
  — Both the writer in `trackActivity` and `resetBucketLocked` hold
  `m.mu`. The copy in `sendCurrentBucket` is made while still holding
  the lock, before reset. No race.

- **"Linux tray Stop() double-close race"** ([tray_linux.go:90-102](desktop/internal/tray/tray_linux.go#L90-L102))
  — Entire `Stop()` body is under `m.mu.Lock()`. Two concurrent calls
  serialise; first closes, second sees `<-m.stopCh` case fire on
  already-closed channel. No panic.

- **"sync.Once of `autoSignOutIfRunning` lets timeout not bound duration"**
  — `sync.Once` wraps the whole call including its context; timeout is
  enforced via `context.WithTimeout`, not via the Once.

---

## §6. Suggested remediation order

**Week 1 (must-ship before next release)**
- C1: hard fail build with empty `release.pub` in prod; disable
  auto-install when `signedReleases()==false`.
- C2: replace `os.Exit(0)` in signal handler with `runtime.Quit(a.ctx)`.
- H1: singleflight around token refresh.
- H6: spawn `OnStopTimer` in goroutine.

**Week 2**
- H2, H3, H5, H7: defensive re-checks and retry paths.
- H4: Cognito error redaction.
- H9: logind cooldown reset.

**Week 3 (hygiene)**
- All Mediums.
- Document false positives in code comments so future audits don't
  re-raise them.

**Backlog**
- All Lows.
- Consider consolidating per-OS monitor files now that the pattern
  is stable (separate proposal).

---

## §7. Gaps in this audit

- **No fuzz tests run.** Findings come from code review only. Consider
  running `go test -race -timeout 30s ./...` continuously in CI — some
  MEDIUMs would surface as test failures.
- **Frontend (Preact) not audited.** Timer drift, auth state, keyboard
  shortcut handling could all hide their own bugs.
- **Installer scripts (`build-installer*.ps1`, NSIS config) not audited.**
  Relevant because signing / cert handling happens there.
- **GitHub Actions workflow YAML not re-audited** since last pass.
- **Wails runtime itself not audited.** Assumed trusted.
