package mcpclient

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/quota"
)

func TestConnectionAccessIsolationAndDisable(t *testing.T) {
	f := setup(t)
	v := f.create(t)
	c, err := f.s.ConnectionAccess(t.Context(), f.p, v.ID)
	must(t, err)
	if c.ID != v.ID || c.Origin() != f.http.URL {
		t.Fatal("connection identity or origin changed", c.ID, c.Origin())
	}
	other := f.p
	other.OwnerID, other.SubjectID = "other", "other"
	if _, err := f.s.ConnectionAccess(t.Context(), other, v.ID); err == nil {
		t.Fatal("another owner accessed a private connection")
	}
	v.Enabled = false
	f.save(t, v)
	if _, err := f.s.ConnectionAccess(t.Context(), f.p, v.ID); core.CodeOf(err) != core.Forbidden {
		t.Fatal("disabled connection remained accessible", err)
	}
}

func TestRunningCallCancellationAndRevalidation(t *testing.T) {
	for _, mode := range []string{"cancel", "permission"} {
		t.Run(mode, func(t *testing.T) {
			f := setup(t)
			started := make(chan struct{}, 1)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			f.add(t, "lookup", "Wait for cancellation", func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				started <- struct{}{}
				// Streamable HTTP need not cancel its handler when the client disconnects.
				<-ctx.Done()
				return nil, ctx.Err()
			})
			v := f.refresh(t, f.create(t))
			v.TimeoutSeconds = 30
			v = f.save(t, v, v.Tools[0].ID)
			c := call(v)
			done := make(chan error, 1)
			go func() { _, err := f.executor.Execute(ctx, c); done <- err }()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("call did not start", ctx.Err())
			}
			if f.s.CancelOperation("other", c.Execution.OperationID) || f.s.CancelOperation(f.p.OwnerID, "other") {
				t.Fatal("cancellation escaped the operation identity")
			}
			f.s.RevalidateRunning(ctx)
			select {
			case err := <-done:
				t.Fatal("authorized operation stopped", err)
			default:
			}
			if mode == "cancel" {
				if !f.s.CancelOperation(f.p.OwnerID, c.Execution.OperationID) {
					t.Fatal("running operation not found")
				}
			} else {
				f.deny.Store(true)
				f.s.RevalidateRunning(ctx)
			}
			select {
			case err := <-done:
				if err == nil || ctx.Err() != nil {
					t.Fatal("operation did not stop before the test deadline", err, ctx.Err())
				}
			case <-ctx.Done():
				t.Fatal("operation was not canceled", ctx.Err())
			}
			used, err := quota.CallTotals(t.Context(), f.s.store, "connection", f.p.OwnerID, v.ID, "", time.Time{}, time.Time{})
			must(t, err)
			if used.Active != 0 || f.s.CancelOperation(f.p.OwnerID, c.Execution.OperationID) {
				t.Fatal("canceled operation retained its running slot", used)
			}
		})
	}
}
