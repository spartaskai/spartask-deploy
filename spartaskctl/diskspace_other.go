//go:build !linux

package main

import "errors"

// freeDiskSpace is measured on Linux servers only; SparTask installs there (Windows runs it in a
// Hyper-V VM with Linux).
func freeDiskSpace(string) (uint64, error) {
	return 0, errors.New("free disk space is measured on Linux servers only")
}
