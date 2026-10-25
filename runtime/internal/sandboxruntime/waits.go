package sandboxruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"time"
)

type Wait struct {
	OwnerID, AssistantID, TaskID, SandboxID, OperationID core.ID
	CreatedAt                                            time.Time
}

const waitBucket = "sandbox.remote_waits"

func (s *Remote) Remember(e core.Execution, id core.ID, err error) {
	if e.TaskID == "" || core.CodeOf(err) != core.Unavailable {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		t, err := task.NewRepository(s.store).Get(ctx, e.Principal.OwnerID, e.TaskID)
		if err != nil {
			return err
		}
		if t.AssistantID != e.AssistantID || (t.State != task.Running && t.State != task.Waiting && t.State != task.Pending) {
			return nil
		}
		created := time.Now().UTC()
		if op, err := operation.NewRepository(s.store).Get(ctx, e.Principal.OwnerID, e.OperationID); err == nil {
			created = op.CreatedAt
		}
		boundary, err := persistence.Load[event.InterruptBoundary](ctx, s.store, "agent.interrupt_boundaries", persistence.Key(e.Principal.OwnerID, e.AssistantID))
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if !boundary.At.IsZero() && !created.After(boundary.At) {
			return nil
		}
		v := t.Version
		t.Version++
		t.State = task.Waiting
		t.WaitingReason = "remote_computer_unavailable"
		t.UpdatedAt = time.Now().UTC()
		if err = task.NewRepository(s.store).Save(ctx, t, v); err != nil {
			return err
		}
		return tx.Put(ctx, waitBucket, persistence.Key(e.Principal.OwnerID, e.TaskID), Wait{e.Principal.OwnerID, e.AssistantID, e.TaskID, id, e.OperationID, created})
	})
}

// Probe outside scheduler transactions and workers, once per node per pass.
func (s *Remote) Tick(ctx context.Context, now time.Time) {
	s.waitMu.Lock()
	if s.waitClosed || s.waitPolling || now.Before(s.nextWaitPoll) {
		s.waitMu.Unlock()
		return
	}
	s.waitPolling = true
	s.nextWaitPoll = now.Add(15 * time.Second)
	s.waitWG.Add(1)
	s.waitMu.Unlock()
	go func() {
		defer s.waitWG.Done()
		defer func() { s.waitMu.Lock(); s.waitPolling = false; s.waitMu.Unlock() }()
		poll, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		rows, err := persistence.Scan[Wait](poll, s.store, waitBucket)
		if err != nil {
			return
		}
		nodes := map[core.ID]bool{}
		for _, w := range rows {
			if poll.Err() != nil {
				return
			}
			ready, known := nodes[w.SandboxID]
			if !known {
				info, err := s.host.Info(poll, core.Principal{Kind: core.ActorOwner, OwnerID: w.OwnerID, SubjectID: w.OwnerID, SessionID: "environment-monitor"}, w.SandboxID)
				ready = err == nil && info.Enabled && info.Storage
				nodes[w.SandboxID] = ready
			}
			if !ready {
				continue
			}
			_ = s.store.Write(poll, func(ctx context.Context, tx persistence.Tx) error {
				key := persistence.Key(w.OwnerID, w.TaskID)
				var current Wait
				if err := tx.Get(ctx, waitBucket, key, &current); err != nil {
					return err
				}
				if current != w {
					return nil
				}
				t, err := task.NewRepository(s.store).Get(ctx, w.OwnerID, w.TaskID)
				if err != nil {
					return err
				}
				a, err := assistant.NewRepository(s.store).Get(ctx, w.OwnerID, w.AssistantID)
				if err != nil {
					return err
				}
				boundary, err := persistence.Load[event.InterruptBoundary](ctx, s.store, "agent.interrupt_boundaries", persistence.Key(w.OwnerID, w.AssistantID))
				if err != nil && !persistence.Missing(err) {
					return err
				}
				st, err := persistence.Load[agent.Status](ctx, s.store, "agent.status", persistence.Key(w.OwnerID, w.AssistantID))
				if err != nil && !persistence.Missing(err) {
					return err
				}
				if t.State == task.Waiting && t.WaitingReason == "remote_computer_unavailable" && a.State == assistant.Active && st.State != agent.Stopped && st.State != agent.Paused && boundary.At.Before(w.CreatedAt) {
					p := core.Principal{Kind: core.ActorOwner, OwnerID: w.OwnerID, SubjectID: w.OwnerID, SessionID: "environment-monitor"}
					if err := s.host.CheckMembership(ctx, w.OwnerID, w.SandboxID); err != nil {
						return tx.Delete(ctx, waitBucket, key)
					}
					if err := s.host.CheckConnection(ctx, p, w.SandboxID); err != nil {
						return err
					}
					id := core.ID("remote_ready_" + digestBytes([]byte(string(w.OperationID)))[:32])
					if err = event.NewWriter(s.store).AppendAssistant(ctx, event.AssistantEvent{ID: id, OwnerID: w.OwnerID, AssistantID: w.AssistantID, TaskID: w.TaskID, Kind: event.ResumeRequested, DeliveryMode: event.WhenIdle, CreatedAt: now}); err != nil {
						return err
					}
				}
				return tx.Delete(ctx, waitBucket, key)
			})
		}
	}()
}
func (s *Remote) Close() {
	s.waitMu.Lock()
	s.waitClosed = true
	s.waitMu.Unlock()
	s.waitWG.Wait()
}

func digestBytes(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
