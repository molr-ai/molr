package sandbox

import (
	"context"
	"reflect"
	"testing"

	"molr.ai/runtime/internal/platform"
)

func TestGPUSnapshotPreservesInventoryWhenSamplingCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	want := []platform.GPUDevice{
		{ID: "GPU-00000000-0000-0000-0000-000000000001", Name: "available", Vendor: "nvidia", MemoryBytes: 8 << 30, Available: true},
		{ID: "GPU-00000000-0000-0000-0000-000000000002", Name: "unavailable", Vendor: "nvidia", MemoryBytes: 4 << 30, Reason: "gpu_probe_failed"},
	}
	runner := &Runner{gpuDevices: append([]platform.GPUDevice(nil), want...)}
	snapshot := runner.GPUSnapshot(ctx)
	if !reflect.DeepEqual(snapshot, want) {
		t.Fatalf("failed sampling changed inventory or unknown usage: %+v", snapshot)
	}
	snapshot[0].ID = "changed"
	snapshot[1].Available = true
	snapshot[1].Reason = ""
	if got := runner.GPUs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot mutation changed startup inventory: %+v", got)
	}
	if got := runner.GPUSnapshot(ctx); !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot mutation affected subsequent snapshot: %+v", got)
	}
}

func TestGPUSnapshotWithoutDevices(t *testing.T) {
	if got := (&Runner{}).GPUSnapshot(t.Context()); len(got) != 0 {
		t.Fatalf("empty inventory returned devices: %+v", got)
	}
}
