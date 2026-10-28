package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/plan"
	"molr.ai/runtime/internal/business/watch"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/shellsession"
)

type watchRun struct {
	cancel context.CancelFunc
	done   chan struct{}
	runID  core.ID
}
type watchRuntime struct {
	store   persistence.Store
	watches *watch.Service
	plans   *plan.Coordinator
	shell   *shellsession.Manager
	mu      sync.Mutex
	active  map[string]*watchRun
	closed  bool
}

func watchKey(w watch.Watch) string { return persistence.Key(w.OwnerID, w.ID) }
func watchShell(w watch.Watch) tools.ShellRequest {
	c := w.Watcher
	q := tools.ShellRequest{ComputeID: c.ComputeID, SandboxID: c.SandboxID, Script: c.Script, Directory: c.Directory, NetworkMode: c.NetworkMode, NetworkIDs: c.NetworkIDs}
	for _, f := range c.Files {
		q.Files = append(q.Files, tools.ShellFile{ResourceID: f.ResourceID, Path: f.Path, Write: f.Write})
	}
	return q
}
func (wr *watchRuntime) fingerprint(ctx context.Context, w watch.Watch) (string, error) {
	rows, err := persistence.Select[policy.Grant](ctx, wr.store, "security.grants", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", w.OwnerID), persistence.Eq("AssistantID", w.AssistantID)}, OrderBy: "key"})
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(rows)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func (wr *watchRuntime) tick(ctx context.Context, now time.Time) error {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	if wr.closed {
		return nil
	}
	rows, err := persistence.Select[watch.Watch](ctx, wr.store, watch.Bucket, persistence.Query{Fields: []string{"ID", "OwnerID", "AssistantID", "Version", "State", "Reason", "RunID", "Paused", "StartAt", "EndAt", "RetryAt", "AuthorizationFingerprint", "PendingCount", "HandlingTaskID"}, Unordered: true})
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, w := range rows {
		k := watchKey(w)
		seen[k] = true
		a, assistantErr := assistant.NewRepository(wr.store).Get(ctx, w.OwnerID, w.AssistantID)
		expired := w.EndAt != nil && !now.Before(*w.EndAt)
		runnable := assistantErr == nil && a.State == assistant.Active && !w.Paused && !expired && w.State != "completed"
		if (w.PendingCount > 0 || w.HandlingTaskID != "") && assistantErr == nil && a.State == assistant.Active && !w.Paused {
			if err := wr.plans.DispatchWatch(ctx, w.OwnerID, w.ID); err != nil && core.CodeOf(err) != core.FailedPrecondition {
				return err
			}
		}
		if active := wr.active[k]; active != nil {
			if !runnable || active.runID != w.RunID {
				active.cancel()
			}
			select {
			case <-active.done:
				delete(wr.active, k)
				continue
			default:
				continue
			}
		}

		if expired && w.State != "completed" {
			if _, err := wr.watches.End(ctx, w.OwnerID, w.ID, w.RunID, "expired", false); err != nil {
				return err
			}
		}
		if !w.Paused && !expired && w.State != "completed" && (assistantErr != nil || a.State != assistant.Active) {
			if w.Reason == "assistant_inactive" && w.RunID == "" {
				continue
			}
			if _, err := wr.watches.End(ctx, w.OwnerID, w.ID, w.RunID, "assistant_inactive", false); err != nil {
				return err
			}
			continue
		}
		if !runnable || w.StartAt != nil && now.Before(*w.StartAt) || w.RetryAt != nil && now.Before(*w.RetryAt) {
			continue
		}
		if w.RunID != "" {
			if _, err := wr.watches.End(ctx, w.OwnerID, w.ID, w.RunID, "runtime_restarted", true); err != nil {
				return err
			}
			if err := wr.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				current, err := watch.Load(ctx, tx.Store(), w.OwnerID, w.ID)
				if err != nil {
					return err
				}
				retry := now.Add(30 * time.Second)
				current.RetryAt = &retry
				current.LastInterruptionAt = &now
				return tx.Put(ctx, watch.Bucket, k, current)
			}); err != nil {
				return err
			}
			continue
		}
		fingerprint, err := wr.fingerprint(ctx, w)
		if err != nil {
			return err
		}
		if w.State == "blocked" && w.Reason != "assistant_inactive" && (w.Reason != "authorization_unavailable" || fingerprint == w.AuthorizationFingerprint) {
			continue
		}
		next, err := wr.watches.Begin(ctx, w.OwnerID, w.ID, w.Version)
		if err != nil {
			if core.CodeOf(err) == core.FailedPrecondition {
				continue
			}
			return err
		}
		runCtx, cancel := context.WithCancel(context.Background())
		run := &watchRun{cancel: cancel, done: make(chan struct{}), runID: next.RunID}
		wr.active[k] = run
		go func(w watch.Watch, fp string) { defer close(run.done); defer cancel(); wr.run(runCtx, w, fp) }(next, fingerprint)
	}
	for k, run := range wr.active {
		if !seen[k] {
			run.cancel()
			select {
			case <-run.done:
				delete(wr.active, k)
				continue
			default:
			}
		}
	}
	return nil
}
func (wr *watchRuntime) close() {
	wr.mu.Lock()
	wr.closed = true
	all := make([]*watchRun, 0, len(wr.active))
	for _, r := range wr.active {
		r.cancel()
		all = append(all, r)
	}
	wr.mu.Unlock()
	for _, r := range all {
		<-r.done
	}
}
func (wr *watchRuntime) run(ctx context.Context, w watch.Watch, fingerprint string) {
	e := core.Execution{Principal: core.Principal{OwnerID: w.OwnerID, SubjectID: w.AssistantID, Kind: core.ActorAssistant, ExecutionID: w.RunID}, AssistantID: w.AssistantID, OperationID: w.RunID}
	runCtx, err := wr.shell.StartWatcher(ctx, e, watchShell(w))
	reason := ""
	retry := true
	if err == nil {
		reason, err = wr.pump(runCtx, e, w)
	}
	if ctx.Err() != nil {
		return
	}
	if err == nil && reason == "" {
		return
	}
	if core.CodeOf(err) == core.Forbidden || core.CodeOf(err) == core.Unauthenticated {
		reason = "authorization_unavailable"
		retry = false
	}
	if reason == "" {
		reason = string(core.ReasonOf(err))
		if reason == "" {
			reason = "watch_failed"
		}
	}
	if reason == "watch_protocol_invalid" || reason == "watch_platform_unsupported" || reason == "watch_queue_full" || reason == "watch_protocol_rate_limit" || reason == "execution_limit" {
		retry = false
	}
	finish, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	// End, failure notification, and the assistant event commit together.
	_ = wr.store.Write(finish, func(ctx context.Context, tx persistence.Tx) error {
		notify, err := wr.watches.End(ctx, w.OwnerID, w.ID, w.RunID, reason, retry)
		if err != nil {
			return err
		}
		current, err := watch.Load(ctx, tx.Store(), w.OwnerID, w.ID)
		if err != nil {
			return err
		}
		if current.Version != w.Version {
			return nil
		}
		current.AuthorizationFingerprint = fingerprint
		if err = tx.Put(ctx, watch.Bucket, watchKey(current), current); err != nil {
			return err
		}
		if !notify {
			return nil
		}
		if err = notification.Sync(ctx, tx.Store(), w.OwnerID, notification.Input{Kind: "watch.failed", Code: reason, Source: core.Object{Kind: "watch", ID: w.ID}, AssistantID: w.AssistantID, Label: w.Title, Episode: string(w.RunID)}, true); err != nil {
			return err
		}
		data, _ := json.Marshal(map[string]any{"PlanID": "watch:" + string(w.ID), "Reason": reason, "Notice": "Continuous observation is blocked. Read the plan; explain the interruption or request only the missing permissions. Do not restart repeatedly. Previously accepted events are retained."})
		return event.NewWriter(tx.Store()).AppendAssistant(ctx, event.AssistantEvent{ID: core.ID("watch_failed_" + string(w.RunID)), OwnerID: w.OwnerID, AssistantID: w.AssistantID, Kind: event.WatchFailed, DeliveryMode: event.WhenIdle, OperationID: w.RunID, Details: data, CreatedAt: time.Now().UTC()})
	})
}
func (wr *watchRuntime) pump(ctx context.Context, e core.Execution, w watch.Watch) (string, error) {
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		_, _ = wr.shell.WatcherIO(cleanup, e, tools.ShellTaskRequest{Action: "stop", TaskID: e.OperationID})
	}()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	cursor := int64(0)
	pending := []byte{}
	started := time.Now()
	healthy := started
	ready := false
	initialized := false
	rateStart := time.Now()
	messages := 0
	start, _ := json.Marshal(map[string]any{"type": "start", "version": 1, "state": w.Checkpoint, "resumed": w.LastInterruptionAt != nil})
	for {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		state, err := wr.shell.WatcherState(ctx, e)
		if err != nil {
			return "", err
		}
		if !initialized {
			_, err = wr.shell.WatcherIO(ctx, e, tools.ShellTaskRequest{Action: "write", TaskID: e.OperationID, Data: string(start) + "\n"})
			if err == nil {
				initialized = true
			}
		}
		raw, readErr := wr.shell.WatcherIO(ctx, e, tools.ShellTaskRequest{Action: "read", TaskID: e.OperationID, Cursor: cursor, Encoding: "base64"})
		hadOutput := false
		if readErr == nil {
			var out struct {
				Output     string
				NextCursor int64
			}
			if json.Unmarshal(raw, &out) != nil {
				return "watch_protocol_invalid", errors.New("invalid output envelope")
			}
			if out.NextCursor < cursor {
				return "watch_protocol_invalid", errors.New("output cursor regressed")
			}
			cursor = out.NextCursor
			decoded, err := base64.StdEncoding.DecodeString(out.Output)
			if err != nil {
				return "watch_protocol_invalid", err
			}
			hadOutput = len(decoded) > 0
			pending = append(pending, decoded...)
			for {
				idx := bytes.IndexByte(pending, '\n')
				if idx < 0 {
					break
				}
				if time.Since(rateStart) >= time.Second {
					rateStart = time.Now()
					messages = 0
				}
				messages++
				if messages > 32 {
					return "watch_protocol_rate_limit", errors.New("watcher message rate exceeded")
				}
				line := append([]byte(nil), pending[:idx]...)
				pending = pending[idx+1:]
				var incoming struct{ Type string }
				_ = json.Unmarshal(line, &incoming)
				if incoming.Type == "complete" && len(bytes.TrimSpace(pending)) > 0 {
					return "watch_protocol_invalid", errors.New("output after complete")
				}
				ack, err := wr.watches.Receive(ctx, w.OwnerID, w.ID, w.RunID, line)
				if err != nil {
					return string(core.ReasonOf(err)), err
				}
				var message struct{ Type string }
				_ = json.Unmarshal(line, &message)
				if message.Type == "ready" {
					ready = true
					_ = notification.Sync(ctx, wr.store, w.OwnerID, notification.Input{Kind: "watch.failed", Source: core.Object{Kind: "watch", ID: w.ID}}, false)
				}
				healthy = time.Now()
				if _, err = wr.shell.WatcherIO(ctx, e, tools.ShellTaskRequest{Action: "write", TaskID: e.OperationID, Data: string(ack) + "\n"}); err != nil && message.Type != "complete" {
					return "watch_input_failed", err
				}
				if message.Type == "complete" {
					if len(bytes.TrimSpace(pending)) > 0 {
						return "watch_protocol_invalid", errors.New("output after complete")
					}
					return "", nil
				}
			}
			if len(pending) > watch.MaxMessage {
				return "watch_protocol_invalid", errors.New("protocol line too large")
			}
		}
		if state.State != "running" && !hadOutput {
			if len(pending) > 0 {
				return "watch_protocol_invalid", errors.New("unterminated protocol message")
			}
			if state.Failure == "forbidden" || state.Failure == "authorization_changed" {
				return "authorization_unavailable", core.NewError(core.Forbidden, "watch authorization expired")
			}
			var result struct {
				Exit struct {
					TimedOut bool
					Reason   string
				}
			}
			_ = json.Unmarshal(state.Result, &result)
			if result.Exit.TimedOut {
				if state.AuthorizationExpiresAt != nil && !time.Now().Before(*state.AuthorizationExpiresAt) {
					return "authorization_unavailable", core.NewError(core.Forbidden, "watch authorization expired")
				}
				return "execution_limit", errors.New("watch execution reached its authorized deadline")
			}
			if state.Failure != "" {
				return state.Failure, errors.New("watch execution ended")
			}
			return "watch_exited", errors.New("watcher exited without complete")
		}
		if !ready && time.Since(started) > 30*time.Second {
			return "watch_start_timeout", errors.New("watcher ready deadline exceeded")
		}
		if ready && w.Watcher.HealthSeconds > 0 && time.Since(healthy) > time.Duration(w.Watcher.HealthSeconds)*time.Second {
			return "watch_health_timeout", errors.New("watcher health deadline exceeded")
		}
		if hadOutput {
			continue
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}
