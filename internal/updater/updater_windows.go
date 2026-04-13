//go:build windows

package updater

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// findPlatformAsset finds the Windows NSIS installer asset.
// Deliberately requires "setup" in the name so the raw Wails binary
// (taskflow-desktop.exe) is never picked — launching that would run the
// new version once but leave the installed copy in Program Files untouched.
func findPlatformAsset(assets []Asset) (downloadURL, fileName string, size int) {
	for _, asset := range assets {
		name := strings.ToLower(asset.Name)
		if strings.HasSuffix(name, ".exe") && strings.Contains(name, "setup") {
			return asset.BrowserDownloadURL, asset.Name, asset.Size
		}
	}
	return "", "", 0
}

// installUpdate launches the downloaded NSIS installer silently with admin
// elevation. ShellExecute with the "runas" verb triggers a Windows UAC prompt
// so the installer gets admin privileges (NSIS script requires it). The "/S"
// flag puts NSIS into silent mode so no pages are shown — the Install section
// runs directly, replacing the binary in place while existing shortcuts and
// autostart registry entries from the previous install are preserved.
func installUpdate(destPath string) error {
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return fmt.Errorf("installer verb: %w", err)
	}
	file, err := windows.UTF16PtrFromString(destPath)
	if err != nil {
		return fmt.Errorf("installer path: %w", err)
	}
	args, err := windows.UTF16PtrFromString("/S")
	if err != nil {
		return fmt.Errorf("installer args: %w", err)
	}
	cwd, err := windows.UTF16PtrFromString(filepath.Dir(destPath))
	if err != nil {
		return fmt.Errorf("installer cwd: %w", err)
	}

	if err := windows.ShellExecute(0, verb, file, args, cwd, windows.SW_SHOWNORMAL); err != nil {
		return fmt.Errorf("failed to launch installer: %w", err)
	}
	return nil
}
