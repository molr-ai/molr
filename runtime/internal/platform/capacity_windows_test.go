package platform

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWindowsCapacitySnapshot(t *testing.T) {
	host := MachineCapacity()
	if host.MemoryBytes <= 0 || host.MemoryUsedBytes == nil || *host.MemoryUsedBytes < 0 || *host.MemoryUsedBytes > host.MemoryBytes {
		t.Fatalf("memory unavailable: %+v", host)
	}
	if runtime.NumCPU() <= 64 && (host.CPUUsagePercent == nil || *host.CPUUsagePercent < 0 || *host.CPUUsagePercent > 100) {
		t.Fatalf("CPU unavailable: %+v", host)
	}
	if host.Processes != 0 {
		t.Fatal("invented process limit")
	}
	if c := FilesystemCapacity("", 0); c != (StorageCapacity{}) {
		t.Fatalf("empty path probed current disk: %+v", c)
	}
	root := t.TempDir()
	for _, path := range []string{root, filepath.Join(root, "missing", "workspace")} {
		c := FilesystemCapacity(path, 0)
		if c.StorageTotalBytes <= 0 || c.StorageAvailableBytes <= 0 || c.StorageUsedBytes+c.StorageAvailableBytes != c.StorageTotalBytes {
			t.Fatalf("invalid disk snapshot: %+v", c)
		}
	}
	if os.Getenv("MOLR_TEST_WINDOWS_NVIDIA") == "1" {
		if len(host.GPUs) == 0 {
			t.Fatal("native NVIDIA inventory missing")
		}
		for _, g := range host.GPUs {
			if g.Name == "" || g.MemoryBytes <= 0 || g.Available || g.Reason != "gpu_platform_unsupported" {
				t.Fatalf("invalid telemetry or GPU authorization: %+v", g)
			}
		}
	}
	data, _ := json.Marshal(host)
	t.Logf("Host capacity: %s", data)
}
