// Package system holds OS-level integrations that don't belong in
// any DDD bounded context — autostart, single-instance locks, etc.
// Per-OS implementations live in autostart_{windows,linux,darwin}.go
// behind build tags.
package system

// Autostart describes the contract every per-OS implementation
// satisfies. Errors propagate up to the Wails binding so the
// frontend's settings drawer can surface a clear "couldn't enable"
// message rather than silently lying about state.
type Autostart interface {
	// Enable registers the app to launch at user login.
	Enable() error
	// Disable removes the registration. Idempotent — disabling a
	// non-registered app is not an error.
	Disable() error
	// Enabled reports whether the app is currently registered.
	Enabled() (bool, error)
}
