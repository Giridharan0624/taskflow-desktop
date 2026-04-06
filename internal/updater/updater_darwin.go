//go:build darwin

package updater

import (
	"fmt"
	"os/exec"
	"strings"
)

// findPlatformAsset finds the macOS .dmg asset.
func findPlatformAsset(assets []Asset) (downloadURL, fileName string, size int) {
	for _, asset := range assets {
		if strings.HasSuffix(strings.ToLower(asset.Name), ".dmg") {
			return asset.BrowserDownloadURL, asset.Name, asset.Size
		}
	}
	return "", "", 0
}

// installUpdate opens the downloaded .dmg for the user to drag-install.
func installUpdate(destPath string) error {
	cmd := exec.Command("open", destPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to open DMG: %w", err)
	}
	return nil
}
