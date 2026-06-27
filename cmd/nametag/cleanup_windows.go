//go:build windows

package main

import "os"

func init() {
	// Windows cannot delete a running binary, so the updater moves the old one
	// to <name>.old to free up the original path. Remove that leftover file now
	// that the new version is running.
	bin, err := os.Executable()
	if err != nil {
		return
	}
	_ = os.Remove(bin + ".old")
}
