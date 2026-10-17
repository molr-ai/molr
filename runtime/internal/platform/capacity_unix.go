//go:build linux || darwin

package platform

import (
	"os"
	"path/filepath"
	"runtime"

	"golang.org/x/sys/unix"
)

func MachineCapacity() HostCapacity {
	c := hostComputeCapacity()
	c.CPUPercent = runtime.NumCPU() * 100
	return c
}

// FilesystemCapacity probes storageRoot, using the nearest existing parent
// when a managed workspace has not been created yet.
func FilesystemCapacity(storageRoot string, reserveBytes int64) StorageCapacity {
	var c StorageCapacity
	if storageRoot == "" {
		return c
	}
	for {
		var st unix.Statfs_t
		err := unix.Statfs(storageRoot, &st)
		if err == nil {
			c.StorageTotalBytes = int64(st.Blocks) * int64(st.Bsize)
			c.StorageUsedBytes = max(0, c.StorageTotalBytes-int64(st.Bfree)*int64(st.Bsize))
			c.StorageBytes = max(0, c.StorageTotalBytes-reserveBytes)
			c.StorageAvailableBytes = max(0, int64(st.Bavail)*int64(st.Bsize)-reserveBytes)
			return c
		}
		if !os.IsNotExist(err) {
			return c
		}
		parent := filepath.Dir(storageRoot)
		if parent == storageRoot {
			return c
		}
		storageRoot = parent
	}
}
