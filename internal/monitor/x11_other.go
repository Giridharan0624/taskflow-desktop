//go:build !linux

package monitor

// CloseX11 is a no-op on non-Linux platforms. Callers in app.go invoke
// it unconditionally during shutdown so cross-platform code doesn't
// need build-tagged branches; the Linux build in x11_linux.go provides
// the real implementation.
func CloseX11() {}
