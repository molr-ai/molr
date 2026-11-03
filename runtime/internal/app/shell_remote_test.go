package app

import (
	"context"
	"encoding/json"
	"errors"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/shellsession"
	"slices"
	"testing"
	"testing/synctest"
	"time"
)

func TestRemoteShellObservationRecordsStartOnceAndKeepsReading(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		changes := make(chan uint64)
		reads, starts, wakes := 0, 0, 0
		control := func(ctx context.Context, q tools.ShellTaskRequest) (json.RawMessage, error) {
			if q.Action != "read" || q.TaskID != "remote-task" {
				t.Errorf("unexpected observation %+v", q)
			}
			reads++
			revision := uint64(1)
			if q.WaitOutput != nil {
				select {
				case revision = <-changes:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return json.Marshal(struct {
				Started        bool
				OutputRevision uint64
			}{true, revision})
		}
		scope := &shellsession.Control{OutputChanged: func() { wakes++ }, Started: func(out, stderr string, gpu []string, control func(context.Context, tools.ShellTaskRequest) (json.RawMessage, error)) error {
			starts++
			if out != "" || stderr != "" || !slices.Equal(gpu, []string{"gpu-0"}) || control == nil {
				t.Error("lost start metadata")
			}
			return nil
		}}
		done := make(chan struct{})
		go func() { defer close(done); observeRemoteShell(ctx, scope, "remote-task", []string{"gpu-0"}, control) }()
		synctest.Wait()
		time.Sleep(time.Hour)
		synctest.Wait()
		if reads != 2 || starts != 1 || wakes != 1 {
			t.Fatal("idle remote shell polled", reads, starts, wakes)
		}
		changes <- 2
		synctest.Wait()
		if reads != 3 || starts != 1 || wakes != 2 {
			t.Fatal("output change not observed", reads, starts, wakes)
		}
		cancel()
		<-done
		time.Sleep(time.Hour)
		if reads != 3 {
			t.Fatal("read after cancellation")
		}
	})
}
func TestRemoteShellObservationRetriesFailedStartUnlessCanceled(t *testing.T) {
	t.Parallel()
	for _, cancelOnFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "retry", true: "cancel"}[cancelOnFailure], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				reads, starts := 0, 0
				control := func(ctx context.Context, q tools.ShellTaskRequest) (json.RawMessage, error) {
					reads++
					if q.WaitOutput != nil {
						<-ctx.Done()
						return nil, ctx.Err()
					}
					return json.RawMessage(`{"Started":true,"OutputRevision":1}`), nil
				}
				scope := &shellsession.Control{Started: func(string, string, []string, func(context.Context, tools.ShellTaskRequest) (json.RawMessage, error)) error {
					starts++
					if starts == 1 {
						if cancelOnFailure {
							cancel()
						}
						return errors.New("start not saved")
					}
					return nil
				}}
				done := make(chan struct{})
				go func() { defer close(done); observeRemoteShell(ctx, scope, "remote-task", nil, control) }()
				synctest.Wait()
				time.Sleep(time.Second)
				synctest.Wait()
				if cancelOnFailure {
					if reads != 1 || starts != 1 {
						t.Fatal(reads, starts)
					}
				} else if reads != 3 || starts != 2 {
					t.Fatal(reads, starts)
				}
				cancel()
				<-done
			})
		})
	}
}
