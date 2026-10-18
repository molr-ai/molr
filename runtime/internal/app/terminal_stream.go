package app

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"time"
)

// Base64 preserves UTF-8 and escape sequences across byte chunks. Output stays
// in the existing bounded archive; emit applies backpressure to slow readers.
func (s *shellTasks) terminalFrames(ctx context.Context, p core.Principal, id, task core.ID, cursor int64, emit func(json.RawMessage) error) error {
	if err := p.RequireOwner(); err != nil {
		return err
	}
	if err := (access{s.store}).assistant(ctx, p, id); err != nil {
		return err
	}
	select {
	case s.forwardSlots <- struct{}{}:
		defer func() { <-s.forwardSlots }()
	default:
		return core.NewError(core.RateLimited, "stream limit")
	}
	watch, err := s.identity.WatchPrincipal(p)
	if err != nil {
		return err
	}
	defer watch.Close()
	last := time.Time{}
	for {
		if _, err := watch.Check(ctx); err != nil {
			return err
		}
		if err := (access{s.store}).assistant(ctx, p, id); err != nil {
			return err
		}
		raw, err := s.manage(ctx, p, id, tools.ShellTaskRequest{Action: "read", TaskID: task, Cursor: cursor, Encoding: "base64"})
		if err != nil {
			return err
		}
		var frame struct {
			Output     string
			NextCursor int64
			Finished   bool
			Terminal   *terminalState
		}
		if err = json.Unmarshal(raw, &frame); err != nil {
			return err
		}
		if frame.Output != "" || time.Since(last) > time.Second || frame.Finished {
			if err := emit(raw); err != nil {
				return err
			}
			last = time.Now()
		}
		cursor = frame.NextCursor
		if frame.Finished && frame.Output == "" {
			return nil
		}
		delay := 100 * time.Millisecond
		if frame.Output != "" {
			delay = 5 * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-watch.Changes():
			timer.Stop()
		case <-timer.C:
		}
	}
}
