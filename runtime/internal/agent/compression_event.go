package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"math"
	"time"

	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

type CompressionRequest struct{ Mutation core.Mutation }
type CompressionResult struct {
	EventID core.ID
	State   string
	Reason  string `json:",omitempty"`
}
type compressionContextKey struct{}

func (e *Engine) Compress(ctx context.Context, p core.Principal, id core.ID, r CompressionRequest) (result CompressionResult, err error) {
	if err = p.RequireOwner(); err != nil {
		return
	}
	if err = e.authorize(ctx, p, id, policy.Write); err != nil {
		return
	}
	if err = r.Mutation.Validate(false); err != nil {
		return
	}
	err = e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		status, err := persistence.Load[Status](ctx, e.store, statusBucket, persistence.Key(p.OwnerID, id))
		if err != nil {
			return err
		}
		if status.State == Stopped || status.State == Paused {
			return core.NewError(core.FailedPrecondition, "assistant execution is stopped")
		}
		key := persistence.Key(p.OwnerID, stableID(string(id), r.Mutation.IdempotencyKey))
		if err := tx.Get(ctx, "agent.compression_requests", key, &result); err == nil {
			return nil
		} else if !persistence.Missing(err) {
			return err
		}
		source, err := e.enqueueCompression(ctx, p.OwnerID, id, true)
		if err != nil {
			return err
		}
		result = CompressionResult{EventID: source.ID, State: "queued"}
		return tx.Put(ctx, "agent.compression_requests", key, result)
	})
	return
}

func (e *Engine) enqueueCompression(ctx context.Context, owner, id core.ID, manual bool) (source event.AssistantEvent, err error) {
	err = e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		// Scan all records rather than allowing pagination to create duplicate work.
		events, err := persistence.Scan[event.AssistantEvent](ctx, e.store, "agent.events")
		if err != nil {
			return err
		}
		boundary, err := persistence.Load[event.InterruptBoundary](ctx, e.store, "agent.interrupt_boundaries", persistence.Key(owner, id))
		if err != nil && !persistence.Missing(err) {
			return err
		}
		for _, existing := range events {
			if existing.OwnerID != owner || existing.AssistantID != id || existing.Kind != event.ContextCompressionRequested || boundary.Blocks(existing) {
				continue
			}
			job, err := persistence.Load[scheduler.Job](ctx, e.store, "agent.jobs", persistence.Key(owner, existing.ID))
			if err != nil && !persistence.Missing(err) {
				return err
			}
			if err == nil && job.State != scheduler.Queued && job.State != scheduler.Running && job.State != scheduler.Retry {
				continue
			}
			var receipt CompressionResult
			if err := tx.Get(ctx, "agent.compression_results", persistence.Key(owner, existing.ID), &receipt); err == nil {
				continue
			} else if !persistence.Missing(err) {
				return err
			}
			if manual && !existing.Manual && job.State != scheduler.Running {
				existing.Manual = true
				if err := tx.Put(ctx, "agent.events", persistence.Key(owner, existing.ID), existing); err != nil {
					return err
				}
				if job.Event.ID != "" {
					job.Event = existing
					job.Version++
					if err := event.SaveJob(ctx, tx, job); err != nil {
						return err
					}
				}
			}
			source = existing
			return nil
		}
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return err
		}
		source = event.AssistantEvent{ID: core.ID(hex.EncodeToString(random[:])), OwnerID: owner, AssistantID: id, Kind: event.ContextCompressionRequested, DeliveryMode: event.AfterStep, Manual: manual, CreatedAt: e.options.Now().UTC()}
		return event.NewWriter(e.store).AppendAssistant(ctx, source)
	})
	return
}

// Automatic detection only enqueues; the same consumer handles both sources.
func (e *Engine) detectCompression(ctx context.Context, t Turn, id core.ID, p *turnProgress) error {
	if t.Event.Kind == event.DreamDue || t.Event.Kind == event.ContextCompressionRequested {
		return nil
	}
	limit, err := e.contextWindow(ctx, t, id)
	if err != nil {
		return err
	}
	tokens, source := contextTokens(*p, id)
	if err := e.recordContextStatus(ctx, t, ContextStatus{ModelID: id, Limit: limit, Tokens: tokens, Source: source, CompressionThreshold: e.options.CompressionThreshold}); err != nil {
		return err
	}
	if tokens < int64(math.Ceil(float64(limit)*e.options.CompressionThreshold)) {
		return nil
	}
	_, err = e.enqueueCompression(ctx, t.Principal.OwnerID, t.Event.AssistantID, false)
	return err
}

func (e *Engine) compressionState(ctx context.Context, t Turn, result CompressionResult) error {
	return e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		key := persistence.Key(t.Principal.OwnerID, t.Event.AssistantID)
		status, err := persistence.Load[Status](ctx, e.store, statusBucket, key)
		if err != nil {
			return err
		}
		if status.ExecutionID != "" && status.ExecutionID != t.Principal.ExecutionID {
			return core.NewError(core.Canceled, "compression execution changed")
		}
		boundary, err := persistence.Load[event.InterruptBoundary](ctx, e.store, "agent.interrupt_boundaries", key)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if boundary.Blocks(t.Event) || status.State == Stopped || status.State == Paused {
			return core.NewError(core.Canceled, "compression stopped")
		}
		status.Compression = &result
		if err := e.saveStatus(ctx, tx, key, status); err != nil {
			return err
		}
		return realtime.NewStore(e.store).Append(ctx, realtime.Visible{ID: stableID(string(result.EventID), result.State, time.Now().String()), OwnerID: t.Principal.OwnerID, Topic: "assistant.state", Object: core.Object{Kind: "assistant", ID: t.Event.AssistantID}, CreatedAt: e.options.Now().UTC()})
	})
}

func (e *Engine) finishCompression(ctx context.Context, t Turn, source event.AssistantEvent, state string, reason string) error {
	result := CompressionResult{EventID: source.ID, State: state, Reason: reason}
	return e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "agent.compression_results", persistence.Key(t.Principal.OwnerID, source.ID), result); err != nil {
			return err
		}
		if source.ID != t.Event.ID {
			job, err := persistence.Load[scheduler.Job](ctx, e.store, "agent.jobs", persistence.Key(t.Principal.OwnerID, source.ID))
			if err != nil && !persistence.Missing(err) {
				return err
			}
			if job.State == scheduler.Canceled {
				return core.NewError(core.Canceled, "compression canceled")
			}
			job.Event = source
			job.ExecutionID = t.Principal.ExecutionID
			job.JoinedTo = ""
			job.State = scheduler.Done
			if state == "failed" {
				job.State = scheduler.Failed
			}
			job.Version++
			if err := event.SaveJob(ctx, tx, job); err != nil {
				return err
			}
		}
		return e.compressionState(ctx, t, result)
	})
}

func (e *Engine) consumeCompression(ctx context.Context, t Turn, id core.ID, p *turnProgress, source event.AssistantEvent) (bool, error) {
	var receipt CompressionResult
	err := e.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Get(ctx, "agent.compression_results", persistence.Key(t.Principal.OwnerID, source.ID), &receipt)
	})
	if err == nil {
		return false, nil
	}
	if !persistence.Missing(err) {
		return false, err
	}
	if len(contextHistory(p.Inputs)) < 2 {
		return false, e.finishCompression(ctx, t, source, "not_needed", "")
	}
	if err := e.compressionState(ctx, t, CompressionResult{EventID: source.ID, State: "running"}); err != nil {
		return false, err
	}
	ctx = context.WithValue(ctx, compressionContextKey{}, source)
	interrupted, err := e.compactContext(ctx, t, id, p, source.Manual)
	if err != nil || interrupted {
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if scheduler.IsDeferred(err) || interrupted {
			_ = e.compressionState(finishCtx, t, CompressionResult{EventID: source.ID, State: "queued"})
		} else {
			reason := string(core.ReasonOf(err))
			if reason == "" {
				reason = string(core.CodeOf(err))
			}
			if finishErr := e.finishCompression(finishCtx, t, source, "failed", reason); finishErr != nil {
				return false, finishErr
			}
		}
		return interrupted, err
	}
	// A below-threshold automatic request is a successful no-op.
	err = e.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Get(ctx, "agent.compression_results", persistence.Key(t.Principal.OwnerID, source.ID), &receipt)
	})
	if persistence.Missing(err) {
		err = e.finishCompression(ctx, t, source, "not_needed", "")
	}
	return false, err
}

func (e *Engine) prepareContext(ctx context.Context, t Turn, id core.ID, p *turnProgress) (bool, error) {
	if err := e.detectCompression(ctx, t, id, p); err != nil {
		return false, err
	}
	jobs, err := scheduler.Pending(ctx, e.store, t.Principal.OwnerID, t.Event.AssistantID, t.Event.ID, e.options.Now(), false)
	if err != nil {
		return false, err
	}
	for _, job := range jobs {
		if job.Event.Kind != event.ContextCompressionRequested {
			continue
		}
		interrupted, err := e.consumeCompression(ctx, t, id, p, job.Event)
		if err != nil || interrupted {
			return interrupted, err
		}
	}
	return false, nil
}
