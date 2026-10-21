//go:build (!linux && !darwin && !windows) || (linux && !amd64 && !arm64)

package sandbox

import (
	"context"
	"molr.ai/runtime/internal/core"
)

func (r *Runner) Terminal(ctx context.Context, e core.Execution, data string, rows, cols uint16) error {
	return unavailable("sandbox_unsupported")
}
