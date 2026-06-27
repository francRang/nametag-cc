//go:build !windows

package updater

import (
	"os"
	"syscall"
)

// replaceBinary makes src executable and renames it to dst.
// The rename is atomic on POSIX systems within a single filesystem.
func replaceBinary(src, dst string) error {
	if err := os.Chmod(src, 0o755); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

// restart replaces the current process image with the binary at path via execve.
// On success this call never returns; the new binary takes over the same PID.
func restart(path string, args []string) error {
	return syscall.Exec(path, args, os.Environ())
}
