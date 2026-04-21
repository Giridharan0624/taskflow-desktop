/**
 * serverClock — a tiny module that maintains the offset between the
 * local `Date.now()` and the backend's authoritative UTC clock.
 *
 * Every API response that carries a `serverTime` ISO string calls
 * `recordServerTime(iso)`; that updates an internal offset. The Timer
 * component (and anywhere else that computes elapsed durations) calls
 * `serverNow()` in place of `Date.now()` so ticks are measured against
 * server time.
 *
 * Why bother:
 *   Two users viewing the same session on two devices were seeing
 *   different elapsed times because each device's Timer computed
 *   `Date.now() - signInAt`. If device A's OS clock was 30 s off NTP
 *   and device B's was synced, their timers drifted by 30 s forever
 *   — the app had no way to notice. With this module both devices
 *   tick relative to the backend's clock, so their displayed times
 *   agree regardless of local clock accuracy.
 *
 * Why not query the server on every tick:
 *   A 1-second tick that round-trips to the server would be absurd.
 *   Instead we piggyback on the polls + mutations that are already
 *   happening (fetch attendance, sign in, sign out, etc.) — each
 *   refreshes the offset for free. No extra network traffic.
 *
 * Low-pass filtering: we average the last N samples to smooth out
 * sample-specific network jitter. A single slow response shouldn't
 * shift the clock by half a second.
 */

const SAMPLE_CAPACITY = 8

// offset such that `serverNow() = Date.now() + offset`
let offset = 0
const samples: number[] = []

export function recordServerTime(iso: string | undefined | null): void {
  if (!iso) return
  const parsed = Date.parse(iso)
  if (!Number.isFinite(parsed)) return
  // Sample = (server - client) captured at response time. Network
  // latency biases this by a few hundred ms (the response was built
  // some time before we received it), but the bias is consistent and
  // small relative to the OS-clock-drift problem we're actually
  // solving. If we wanted sub-100 ms accuracy we'd do an NTP-style
  // round-trip measurement; not worth the complexity here.
  const sample = parsed - Date.now()
  samples.push(sample)
  if (samples.length > SAMPLE_CAPACITY) samples.shift()
  // Median of the recent samples — robust against a single slow
  // response that would skew a naive average.
  const sorted = [...samples].sort((a, b) => a - b)
  offset = sorted[Math.floor(sorted.length / 2)]
}

export function serverNow(): number {
  return Date.now() + offset
}

/** Elapsed milliseconds between an ISO timestamp and server-now.
 *  Used by Timer.tsx + getSessionHours so both derive their clock
 *  from the same reference. */
export function elapsedSince(iso: string | null | undefined): number {
  if (!iso) return 0
  const start = Date.parse(iso)
  if (!Number.isFinite(start)) return 0
  return Math.max(0, serverNow() - start)
}

/** Test seam — not exported in production code paths. */
export function _resetForTests(): void {
  offset = 0
  samples.length = 0
}
