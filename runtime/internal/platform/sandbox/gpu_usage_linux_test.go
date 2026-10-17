//go:build linux && (amd64 || arm64)

package sandbox

import (
	"molr.ai/runtime/internal/platform"
	"testing"
)

func TestGPUUsageSnapshotParsesUnknownAndZero(t *testing.T) {
	id2 := "GPU-00000000-0000-0000-0000-000000000002"
	usage, err := parseNVIDIAUsage([]byte(fixtureGPU + ", 25, 4096\n" + id2 + ", 0, 0\n"))
	if err != nil {
		t.Fatal(err)
	}
	first, second := usage[fixtureGPU], usage[id2]
	if first.utilization == nil || *first.utilization != 25 || first.memory == nil || *first.memory != 4<<30 {
		t.Fatal("incorrect usage units", first)
	}
	if second.utilization == nil || *second.utilization != 0 || second.memory == nil || *second.memory != 0 {
		t.Fatal("zero usage treated as unknown")
	}
	for _, row := range []string{"N/A, 4096", "[Not Supported], 4096", "NaN, 4096", "101, 4096", "-1, 4096"} {
		parsed, err := parseNVIDIAUsage([]byte(fixtureGPU + ", " + row))
		if err != nil || parsed[fixtureGPU].utilization != nil || parsed[fixtureGPU].memory == nil {
			t.Fatal("unknown utilization must not hide valid memory", parsed, err)
		}
	}
	for _, row := range []string{"25, N/A", "25, -1", "25, 999999999999"} {
		parsed, err := parseNVIDIAUsage([]byte(fixtureGPU + ", " + row))
		if err != nil || parsed[fixtureGPU].utilization == nil || parsed[fixtureGPU].memory != nil {
			t.Fatal("invalid memory must not hide valid utilization", parsed, err)
		}
	}
	for _, row := range []string{"invalid, 25, 4096", fixtureGPU + ", 25", fixtureGPU + ", 25, 4096\n" + fixtureGPU + ", 50, 4096"} {
		if _, err := parseNVIDIAUsage([]byte(row)); err == nil {
			t.Fatal("invalid GPU identity accepted", row)
		}
	}
}

func TestGPUUsageDoesNotChangeInventoryOrAvailability(t *testing.T) {
	usage, err := parseNVIDIAUsage([]byte(fixtureGPU + ", 20, 4096\nGPU-00000000-0000-0000-0000-000000000002, 90, 4096"))
	if err != nil {
		t.Fatal(err)
	}
	devices := []platform.GPUDevice{{ID: fixtureGPU, Name: "Example GPU", MemoryBytes: 8 << 30, Available: false, Reason: "gpu_probe_failed"}}
	applyGPUUsage(devices, usage)
	if len(devices) != 1 || devices[0].ID != fixtureGPU || devices[0].Available || devices[0].Reason != "gpu_probe_failed" || devices[0].MemoryUsedBytes == nil {
		t.Fatal("sampling changed device scope or capability", devices)
	}
	devices[0].MemoryBytes = 2 << 30
	applyGPUUsage(devices, usage)
	if devices[0].MemoryUsedBytes != nil {
		t.Fatal("impossible memory sample displayed")
	}
	applyGPUUsage(devices, nil)
	if devices[0].UtilizationPercent != nil || devices[0].MemoryUsedBytes != nil {
		t.Fatal("missing device retained old usage")
	}
}
