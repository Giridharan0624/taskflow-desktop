//go:build windows

package queue

import (
	"fmt"
	"os"
	"path/filepath"
)

func appDataRoot() (string, error) {
	root := os.Getenv("APPDATA")
	if root == "" {
		return "", fmt.Errorf("APPDATA not set")
	}
	return filepath.Join(root, "TaskFlow"), nil
}
