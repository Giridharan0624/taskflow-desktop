//go:build windows

package updater

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// findPlatformAsset finds the Windows .exe installer asset.
func findPlatformAsset(assets []Asset) (downloadURL, fileName string, size int) {
	for _, asset := range assets {
		if strings.HasSuffix(strings.ToLower(asset.Name), ".exe") {
			return asset.BrowserDownloadURL, asset.Name, asset.Size
		}
	}
	return "", "", 0
}

// installUpdate launches the downloaded .exe installer.
func installUpdate(destPath string) error {
	cmd := exec.Command(destPath)
	cmd.Dir = filepath.Dir(destPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to launch installer: %w", err)
	}
	return nil
}
