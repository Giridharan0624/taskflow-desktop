//go:build linux

package system

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Linux desktop autostart is the XDG-spec ~/.config/autostart/*.desktop
// pattern. Every mainstream desktop environment (GNOME, KDE, XFCE,
// Cinnamon) reads this directory at session start. Survives upgrades
// and doesn't need root.
//
// We write a minimal .desktop file that points at the running
// executable. If the user moves the binary, the entry stops working
// — same trade-off as the Windows registry path. A future iteration
// could rewrite the file on every Enable() call so it always points
// at the latest exe location.
const autostartFilename = "taskflow-desktop.desktop"

type linuxAutostart struct{}

func New() Autostart { return linuxAutostart{} }

func autostartPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		dir = filepath.Join(home, ".config")
	}
	autostart := filepath.Join(dir, "autostart")
	if err := os.MkdirAll(autostart, 0700); err != nil {
		return "", fmt.Errorf("create autostart dir: %w", err)
	}
	return filepath.Join(autostart, autostartFilename), nil
}

func (linuxAutostart) Enable() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve exe path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	path, err := autostartPath()
	if err != nil {
		return err
	}
	// Hidden=false is the default but we make it explicit so users
	// who inspect the file see the intent. X-GNOME-Autostart-enabled
	// is GNOME-specific but harmless on other DEs.
	body := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=TaskFlow Desktop
Comment=Time tracker
Exec="%s"
Terminal=false
Hidden=false
X-GNOME-Autostart-enabled=true
`, exe)
	return os.WriteFile(path, []byte(body), 0600)
}

func (linuxAutostart) Disable() error {
	path, err := autostartPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove autostart file: %w", err)
	}
	return nil
}

func (linuxAutostart) Enabled() (bool, error) {
	path, err := autostartPath()
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
