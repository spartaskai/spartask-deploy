package main

import (
	"fmt"
	"syscall"
)

// freeDiskSpace returns the bytes available in the file system holding path.
func freeDiskSpace(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, fmt.Errorf("read the free space of %s: %w", path, err)
	}
	return st.Bavail * uint64(st.Bsize), nil
}
