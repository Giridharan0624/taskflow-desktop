//go:build darwin

package queue

import (
	"os"
	"path/filepath"
)

func appDataRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "TaskFlow"), nil
}
