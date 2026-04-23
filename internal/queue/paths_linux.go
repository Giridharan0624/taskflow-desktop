//go:build linux

package queue

import (
	"os"
	"path/filepath"
)

func appDataRoot() (string, error) {
	// XDG basedir first — matches what modern Linux apps do and
	// respects user config of $XDG_DATA_HOME.
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "taskflow"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "taskflow"), nil
}
