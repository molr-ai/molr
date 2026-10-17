package sandbox

import (
	"context"
	"molr.ai/runtime/internal/platform"
	"slices"
)

type gpuNode struct {
	Path  string
	Minor uint32
}

// GPUs reports startup probes separately from CPU sandbox capabilities.
func (r *Runner) GPUs() []platform.GPUDevice {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.gpuDevices)
}

// GPUSnapshot adds current usage to a copy of the authorized device inventory.
// Sampling failures never change device scope or startup isolation capability.
func (r *Runner) GPUSnapshot(ctx context.Context) []platform.GPUDevice {
	devices := r.GPUs()
	sampleGPUUsage(ctx, devices)
	return devices
}
