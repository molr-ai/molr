package dream

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/scheduler"
)

func TestMalformedMemoryStillSchedulesMaintenance(t *testing.T) {
	for _, body := range []string{
		"---\nscope: assistant\ncreated: 2026-09-28\n---\nUseful note.",
		"---\nstate: broken\ncreated_at: yesterday\n---\nUseful note.",
		"---\nstate: [broken YAML\n---\nUseful note.",
		strings.Repeat("x", 81<<10),
	} {
		t.Run(body[:min(len(body), 20)], func(t *testing.T) {
			f := setup(t)
			f.now = f.now.Add(-time.Hour).Add(23 * time.Second)
			dir := filepath.Join(f.root, "owner", "work", ".agents", "memory")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if err := f.s.Tick(t.Context(), f.now); err != nil {
				t.Fatal(err)
			}
			runs, err := f.s.Runs(t.Context(), f.p, "assistant")
			if err != nil || len(runs) != 1 || runs[0].State != "queued" || !runs[0].Maintenance {
				t.Fatalf("maintenance not admitted: %+v %v", runs, err)
			}
			worker := scheduler.NewService(f.db, scheduler.Options{Handle: func(context.Context, scheduler.Claim) error { return nil }, Now: func() time.Time { return f.now }, Tick: f.s.Tick})
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			if err := worker.Run(ctx); err != nil {
				t.Fatal("content terminated scheduler", err)
			}
			if ctx.Err() == nil {
				t.Fatal("scheduler stopped before cancellation")
			}
		})
	}
}
