//go:build linux

package updater

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// findPlatformAsset finds the Linux .AppImage asset.
//
// We only ship AppImage builds for auto-update: .deb/.rpm releases
// are the package manager's job (auto-install would need sudo and
// would conflict with dpkg's ownership of the installed binary —
// see installUpdate below). If we ever add a self-signed .deb auto-
// update path it would pick a different asset here.
func findPlatformAsset(assets []Asset) (downloadURL, fileName string, size int) {
	for _, asset := range assets {
		if strings.HasSuffix(strings.ToLower(asset.Name), ".appimage") {
			return asset.BrowserDownloadURL, asset.Name, asset.Size
		}
	}
	return "", "", 0
}

// installOrigin describes where the running binary lives, which
// determines whether auto-update is safe.
type installOrigin int

const (
	originUnknown   installOrigin = iota
	originAppImage                // running inside an AppImage bundle
	originUserLocal               // user-placed binary somewhere under $HOME
	originSystem                  // dpkg/rpm/snap in /usr, /opt, /snap
)

// detectInstallOrigin inspects the running process to decide how (or
// whether) it's safe to replace the binary on disk.
//
// The AppImage runtime always exports $APPIMAGE to the bundle path
// when it spawns the inner app — that's the cheap authoritative
// signal. For everything else we fall through to a path-prefix
// check: system directories mean a package manager owns this, and
// anywhere else we assume the user dropped a standalone binary.
func detectInstallOrigin() installOrigin {
	if p := os.Getenv("APPIMAGE"); p != "" {
		return originAppImage
	}
	exe, err := os.Executable()
	if err != nil {
		return originUnknown
	}
	// Resolve symlinks so a shortcut in ~/.local/bin pointing into
	// an AppImage or into /usr/bin gets classified correctly.
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	lower := strings.ToLower(exe)
	if strings.HasSuffix(lower, ".appimage") {
		// User launched the AppImage directly (no AppRun shim).
		// Still self-updatable.
		return originAppImage
	}
	for _, prefix := range []string{"/usr/", "/opt/", "/snap/"} {
		if strings.HasPrefix(exe, prefix) {
			return originSystem
		}
	}
	return originUserLocal
}

// installUpdate replaces the running binary with destPath and
// relaunches. Behavior depends on where this build lives on disk:
//
//   - originSystem (dpkg/rpm/snap) → refuse with ErrPackageManaged.
//     The caller surfaces a notification telling the user to run
//     their package manager. Auto-replacing would either fail (no
//     write permission on /usr/bin) or succeed while leaving the
//     package manager's database out of sync.
//
//   - originAppImage / originUserLocal / originUnknown → atomically
//     rename the new file onto the running executable's path and
//     spawn it detached, then return so the caller (runtime.Quit)
//     drains goroutines before the old process exits.
//
// Atomic-rename note: Linux permits renaming over a running
// executable. The kernel keeps the old inode alive for every process
// that has it memory-mapped; anyone opening the path afterwards gets
// the new file. This is the standard self-update pattern used by
// tools like go itself, terraform, kubectl. See V3-Mdeb.
func installUpdate(destPath string) error {
	switch detectInstallOrigin() {
	case originSystem:
		return ErrPackageManaged
	}

	targetPath, err := runningExePath()
	if err != nil {
		return fmt.Errorf("resolve running exe: %w", err)
	}

	// Copy destPath → sibling-of-target so rename is same-filesystem
	// (rename across filesystems returns EXDEV). A hidden dotfile
	// name keeps the in-flight update invisible in file managers.
	targetDir := filepath.Dir(targetPath)
	tmpName := "." + filepath.Base(targetPath) + ".update.tmp"
	tmpPath := filepath.Join(targetDir, tmpName)

	// If a previous aborted install left a leftover tmp file, remove
	// it so our O_EXCL create below doesn't spuriously fail.
	_ = os.Remove(tmpPath)

	if err := copyFileSync(destPath, tmpPath); err != nil {
		return fmt.Errorf("stage update next to running exe: %w", err)
	}
	staged := true
	defer func() {
		if staged {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("chmod staged update: %w", err)
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("rename onto running exe: %w", err)
	}
	staged = false // rename consumed the tmp file

	// Relaunch detached via a new session. Setsid ensures the child
	// survives this process exiting and doesn't inherit our
	// controlling terminal (matters when the user ran the AppImage
	// from a terminal window).
	cmd := exec.Command(targetPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("relaunch updated binary: %w", err)
	}
	// Detach so we don't zombie-wait the child.
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
	log.Printf("update staged at %s; child PID %d took over — current process will exit via runtime.Quit",
		targetPath, cmd.Process.Pid)
	return nil
}

// runningExePath returns the path whose file we need to replace.
// Prefers $APPIMAGE (set by the AppImage runtime to the bundle path)
// so updating an extracted/renamed AppImage still hits the file the
// user's launcher points at, not the shim inside the mounted FUSE
// image.
func runningExePath() (string, error) {
	if p := os.Getenv("APPIMAGE"); p != "" {
		return p, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
}

// copyFileSync streams src→dst and fsyncs dst before close. The
// fsync is load-bearing: if the system power-loses between our
// io.Copy and the subsequent os.Rename, dst could otherwise be an
// empty file that the rename then publishes as the running exe —
// bricking the app on next launch. fsync makes the on-disk bytes a
// prerequisite for the rename to even reach this function's return.
func copyFileSync(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
