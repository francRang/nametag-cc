//go:build windows

package updater

import (
	"fmt"
	"os"
	"os/exec"
)

// replaceBinary replaces dst with src on Windows, where the OS locks running
// executables. The current binary is first moved to dst+".old" to vacate its
// path, then src is renamed into place. If the second rename fails, we attempt
// to restore dst from the backup.
func replaceBinary(src, dst string) error {
	old := dst + ".old"
	// Remove any leftover backup from a previous update attempt.
	_ = os.Remove(old)

	// Move the running binary aside.
	if err := os.Rename(dst, old); err != nil {
		return fmt.Errorf("backing up current binary: %w", err)
	}

	// Install the new binary.
	if err := os.Rename(src, dst); err != nil {
		// Best-effort restore so the user isn't left without a working binary.
		_ = os.Rename(old, dst)
		return fmt.Errorf("installing new binary: %w", err)
	}
	return nil
}

// restart spawns a new instance of the binary at path with the given arguments
// (args[1:] are passed; args[0] is the program name which exec.Command sets
// automatically). After starting the child it exits this process. Returns an
// error only if the child process cannot be started.
func restart(path string, args []string) error {
	cmd := exec.Command(path, args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting new process: %w", err)
	}
	os.Exit(0)
	return nil // unreachable
}
