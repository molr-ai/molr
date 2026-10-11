package sandbox

import (
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
