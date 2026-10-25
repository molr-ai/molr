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
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		reads, starts := 0, 0
		control := func(ctx context.Context, q tools.ShellTaskRequest) (json.RawMessage, error) {
			if q.Action != "read" || q.TaskID != "remote-task" {
				t.Errorf("unexpected observation: %+v", q)
			}
			reads++
			switch reads {
			case 1, 6:
				return nil, errors.New("temporary read failure")
			case 2:
				return json.RawMessage(`{"Started":false}`), nil
			case 3:
				return json.RawMessage(`invalid`), nil
			default:
				return json.RawMessage(`{"Started":true}`), nil
			}
		}
		scope := &shellsession.Control{Started: func(out, stderr string, gpuIDs []string, control func(context.Context, tools.ShellTaskRequest) (json.RawMessage, error)) error {
			starts++
			if out != "" || stderr != "" || !slices.Equal(gpuIDs, []string{"gpu-0"}) || control == nil {
				t.Error("start lost output defaults, GPU devices or terminal control")
			}
			return nil
		}}
		done := make(chan struct{})
		go func() {
			defer close(done)
			observeRemoteShell(ctx, scope, "remote-task", []string{"gpu-0"}, control)
		}()
		for i := 1; i <= 8; i++ {
			synctest.Wait()
			wantStarts := 0
			if i >= 4 {
				wantStarts = 1
			}
			if reads != i || starts != wantStarts {
				t.Fatalf("poll %d: reads=%d, starts=%d; want %d, %d", i, reads, starts, i, wantStarts)
			}
			if i < 8 {
				time.Sleep(time.Second)
			}
		}
		cancel()
		<-done
		time.Sleep(2 * time.Second)
		if reads != 8 {
			t.Fatal("observation continued after cancellation")
		}
	})
}

func TestRemoteShellObservationRetriesFailedStartUnlessCanceled(t *testing.T) {
	for _, cancelOnFailure := range []bool{false, true} {
		name := "retry"
		if cancelOnFailure {
			name = "cancel"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				reads, starts := 0, 0
				control := func(context.Context, tools.ShellTaskRequest) (json.RawMessage, error) {
					reads++
					return json.RawMessage(`{"Started":true}`), nil
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
				go func() {
					defer close(done)
					observeRemoteShell(ctx, scope, "remote-task", nil, control)
				}()
				synctest.Wait()
				if reads != 1 || starts != 1 {
					t.Fatalf("initial observation: reads=%d, starts=%d", reads, starts)
				}
				for i := 0; i < 3; i++ {
					time.Sleep(time.Second)
					synctest.Wait()
				}
				cancel()
				<-done
				wantReads, wantStarts := 4, 2
				if cancelOnFailure {
					wantReads, wantStarts = 1, 1
				}
				if reads != wantReads || starts != wantStarts {
					t.Fatalf("reads=%d, starts=%d; want %d, %d", reads, starts, wantReads, wantStarts)
				}
			})
		})
	}
}
