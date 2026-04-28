/**
 * Tray-notification gateway.
 *
 * Every transient event that wants to surface a tray balloon goes
 * through here. The user's `notifications` preference (set in the
 * Settings drawer) decides whether the balloon actually fires:
 *
 *   - "all"         → every notification fires
 *   - "errors-only" → only `kind: "error"` fires
 *   - "off"         → nothing fires
 *
 * The Go side's tray.ShowBalloon is unconditional; we centralize
 * the policy check here so individual call sites don't have to
 * thread the settings store. They just call `notify(...)`.
 */

import { getSettings } from "./settings";

export type NotifyKind = "info" | "error";

export function notify(kind: NotifyKind, title: string, message: string): void {
  const policy = getSettings().notifications;
  if (policy === "off") return;
  if (policy === "errors-only" && kind !== "error") return;
  // Best-effort — IPC failures during shutdown shouldn't bubble.
  window.go.main.App.ShowTrayNotification(title, message).catch(() => {});
}
