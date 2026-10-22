//go:build linux || darwin || windows

package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMachineCapacity(t *testing.T) {
	root := t.TempDir()
	host := MachineCapacity()
	if host.CPUPercent <= 0 || host.MemoryBytes <= 0 || (runtime.GOOS != "windows" && host.Processes <= 0) {
		t.Fatalf("invalid host capacity: %+v", host)
	}
	c := FilesystemCapacity(root, 0)
	if c.StorageBytes <= 0 || c.StorageAvailableBytes > c.StorageBytes || c.StorageUsedBytes < 0 || c.StorageUsedBytes > c.StorageTotalBytes {
		t.Fatalf("invalid machine capacity: %+v", c)
	}
	reserve := int64(256 << 20)
	reserved := FilesystemCapacity(filepath.Join(root, "not-created", "workspace"), reserve)
	if reserved.StorageBytes != max(0, c.StorageBytes-reserve) || reserved.StorageTotalBytes != c.StorageTotalBytes {
		t.Fatalf("reserve or nearest filesystem ignored: %+v", reserved)
	}
	exhausted := FilesystemCapacity(root, c.StorageBytes+1)
	if exhausted.StorageBytes != 0 || exhausted.StorageAvailableBytes != 0 {
		t.Fatalf("negative available space: %+v", exhausted)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if invalid := FilesystemCapacity(filepath.Join(file, "child"), 0); invalid.StorageBytes != 0 {
		t.Fatalf("invalid root fell back to another filesystem: %+v", invalid)
	}
}
