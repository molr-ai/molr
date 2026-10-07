//go:build !linux || (!amd64 && !arm64)

package sandbox

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
)

func (r *Runner) checkDirect() error { return unavailable("sandbox_platform_unsupported") }
func (r *Runner) startDirect(context.Context, core.Execution, platform.Command) (*process, error) {
	return nil, r.checkDirect()
}
