//go:build !linux || (!amd64 && !arm64)

package sandbox

import (
	"context"
	"molr.ai/runtime/internal/platform"
)

func (r *Runner) checkGPUAccess(c platform.Command) error {
	if len(c.GPUDeviceIDs) > 0 {
		return unavailable("gpu_platform_unsupported")
	}
	return nil
}

func sampleGPUUsage(context.Context, []platform.GPUDevice) {}
