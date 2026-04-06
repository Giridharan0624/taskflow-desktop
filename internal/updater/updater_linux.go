//go:build linux

package updater

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// findPlatformAsset finds the Linux .AppImage asset.
func findPlatformAsset(assets []Asset) (downloadURL, fileName string, size int) {
	for _, asset := range assets {
		if strings.HasSuffix(strings.ToLower(asset.Name), ".appimage") {
			return asset.BrowserDownloadURL, asset.Name, asset.Size
		}
	}
	return "", "", 0
}

// installUpdate replaces the current binary with the downloaded AppImage.
func installUpdate(destPath string) error {
	// Make executable
	if err := os.Chmod(destPath, 0755); err != nil {
		return fmt.Errorf("chmod failed: %w", err)
	}

	// Launch the new AppImage
	cmd := exec.Command(destPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to launch AppImage: %w", err)
	}
	return nil
}
