package platform_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/platform"
)

func TestWindowsServiceLifecycle(t *testing.T) {
	directory := t.TempDir()
	other := filepath.Join(directory, "other")
	if platform.ServiceRunning(directory) {
		t.Fatal("service exists before startup")
	}
	ctx, closeService, err := platform.ServiceContext(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeService)
	if !platform.ServiceRunning(directory) || platform.ServiceRunning(other) {
		t.Fatal("service identity does not match its directory")
	}
	if err := platform.StopService(other); err == nil {
		t.Fatal("stopped a nonexistent service")
	}
	if ctx.Err() != nil {
		t.Fatal("unrelated stop canceled service")
	}
	if err := platform.StopService(directory); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
		if ctx.Err() != context.Canceled {
			t.Fatal(ctx.Err())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not cancel the service context")
	}
}
