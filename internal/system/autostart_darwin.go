//go:build darwin

package system

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// macOS uses LaunchAgents — per-user plist files in
// ~/Library/LaunchAgents/. The file is loaded at user login by
// launchd. RunAtLoad=true means launch immediately when the agent
// is loaded; KeepAlive=false means don't restart on quit.
//
// Compared to the older "Login Items" approach via System
// Preferences, LaunchAgents survives macOS upgrades cleanly and
// doesn't require Accessibility permissions.
const (
	launchAgentLabel    = "com.neurostack.taskflow.desktop"
	launchAgentFilename = "com.neurostack.taskflow.desktop.plist"
)

type darwinAutostart struct{}

func New() Autostart { return darwinAutostart{} }

func launchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	dir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	return filepath.Join(dir, launchAgentFilename), nil
}

func (darwinAutostart) Enable() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve exe path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	path, err := launchAgentPath()
	if err != nil {
		return err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <false/>
    <key>ProcessType</key>
    <string>Interactive</string>
</dict>
</plist>
`, launchAgentLabel, exe)
	return os.WriteFile(path, []byte(plist), 0600)
}

func (darwinAutostart) Disable() error {
	path, err := launchAgentPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove LaunchAgent: %w", err)
	}
	return nil
}

func (darwinAutostart) Enabled() (bool, error) {
	path, err := launchAgentPath()
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
