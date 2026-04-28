//go:build windows

package system

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

// Windows uses the per-user Run key in the registry. No admin
// rights required, survives reboots, removed cleanly on uninstall
// when the registry uninstaller runs.
//
//	HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Run
//	  Name:  TaskFlowDesktop
//	  Value: "<absolute path to taskflow-desktop.exe>"
const (
	winRunKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	winValueName  = "TaskFlowDesktop"
)

type windowsAutostart struct{}

func New() Autostart { return windowsAutostart{} }

func (windowsAutostart) Enable() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve exe path: %w", err)
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, winRunKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open Run key: %w", err)
	}
	defer k.Close()
	// Quote the path so a folder containing spaces (e.g. "Program
	// Files") doesn't get interpreted as multiple arguments by the
	// shell at boot.
	value := `"` + exe + `"`
	if err := k.SetStringValue(winValueName, value); err != nil {
		return fmt.Errorf("set Run value: %w", err)
	}
	return nil
}

func (windowsAutostart) Disable() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, winRunKeyPath, registry.SET_VALUE)
	if err != nil {
		// Not opening = we couldn't write. Treat the special
		// "key doesn't exist" case as "already disabled".
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open Run key: %w", err)
	}
	defer k.Close()
	if err := k.DeleteValue(winValueName); err != nil {
		// Idempotent — missing value isn't an error.
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("delete Run value: %w", err)
	}
	return nil
}

func (windowsAutostart) Enabled() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, winRunKeyPath, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer k.Close()
	_, _, err = k.GetStringValue(winValueName)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
