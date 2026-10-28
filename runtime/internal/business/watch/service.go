package watch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"strings"
	"time"
)

func newID() core.ID {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return core.ID(hex.EncodeToString(b[:]))
}
func (s *Service) authorize(ctx context.Context, p core.Principal, id core.ID, action policy.Action) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if s.options.Authorizer == nil {
		return core.NewError(core.Unavailable, "authorization unavailable")
	}
	return s.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "watch", ID: id}, Action: action})
}
func validate(r Request) error {
	c := r.Watcher
	if r.AssistantID.Validate() != nil || strings.TrimSpace(r.Title) == "" || len(r.Title) > 4096 || len(r.Instructions) > 65536 || strings.TrimSpace(r.Instructions) == "" {
		return core.Invalid("watch", "assistant, title and instructions required")
	}
	if c.ComputeID.Validate() != nil || c.SandboxID != "" && c.SandboxID.Validate() != nil || strings.TrimSpace(c.Script) == "" || len(c.Script) > 65536 || strings.ContainsRune(c.Script, 0) || len(c.Files) > 32 || len(c.NetworkIDs) > 32 || c.HealthSeconds < 0 || c.HealthSeconds > 86400 {
		return core.Invalid("watcher", "invalid watcher configuration")
	}
	if c.NetworkMode != "" && c.NetworkMode != "auto" && c.NetworkMode != "none" || c.NetworkMode == "none" && len(c.NetworkIDs) > 0 {
		return core.Invalid("network_mode", "invalid watcher network selection")
	}
	for _, f := range c.Files {
		if f.ResourceID.Validate() != nil {
			return core.Invalid("files", "invalid resource")
		}
	}
	for _, id := range c.NetworkIDs {
		if id.Validate() != nil {
			return core.Invalid("network_ids", "invalid resource")
		}
	}
	if r.StartAt != nil && r.EndAt != nil && !r.EndAt.After(*r.StartAt) {
		return core.Invalid("end_at", "must follow start_at")
	}
	return nil
}
func (s *Service) Get(ctx context.Context, p core.Principal, id core.ID) (Watch, error) {
	if err := s.authorize(ctx, p, id, policy.Read); err != nil {
		return Watch{}, err
	}
	w, err := Load(ctx, s.store, p.OwnerID, id)
	if err == nil && p.Kind == core.ActorAssistant && w.AssistantID != p.SubjectID {
		return Watch{}, core.NewError(core.Forbidden, "watch belongs to another assistant")
	}
	return w, err
}
func (s *Service) Save(ctx context.Context, p core.Principal, r Request) (Watch, error) {
	var out Watch
	if err := validate(r); err != nil {
		return out, err
	}
	if err := r.Mutation.Validate(r.ID != ""); err != nil {
		return out, err
	}
	if err := s.authorize(ctx, p, r.ID, policy.Write); err != nil {
		return out, err
	}
	if s.options.CheckAssistant != nil && (!r.Paused || r.ID == "") {
		if err := s.options.CheckAssistant(ctx, p, r.AssistantID); err != nil {
			return out, err
		}
	}
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		digest, err := persistence.PayloadDigest(r)
		if err != nil {
			return err
		}
		out, err = persistence.MutateReceipt(ctx, tx, "business.watch-receipts", persistence.Key(p.OwnerID, core.ID(r.Mutation.IdempotencyKey)), digest, func(ctx context.Context) (Watch, error) {
			now := time.Now().UTC()
			w := Watch{ID: newID(), OwnerID: p.OwnerID, CreatedAt: now, Checkpoint: json.RawMessage("null"), Pending: []Signal{}, Recent: []Signal{}, State: "pending"}
			if r.ID != "" {
				var err error
				w, err = Load(ctx, tx.Store(), p.OwnerID, r.ID)
				if err != nil {
					return w, err
				}
				if w.Version != r.Mutation.ExpectedVersion {
					return w, core.NewError(core.Conflict, "watch changed")
				}
				if w.AssistantID != r.AssistantID {
					return w, core.Invalid("assistant_id", "assignee cannot change")
				}
				old, _ := json.Marshal(w.Watcher)
				next, _ := json.Marshal(r.Watcher)
				if string(old) != string(next) {
					w.Checkpoint = json.RawMessage("null")
				}
				w.RunID = ""
			}
			w.AssistantID = r.AssistantID
			w.Title = r.Title
			w.Instructions = r.Instructions
			w.Watcher = r.Watcher
			w.StartAt = r.StartAt
			w.EndAt = r.EndAt
			w.Paused = r.Paused
			w.Version++
			w.UpdatedAt = now
			w.Failures = 0
			w.FailureNotified = false
			w.Reason = ""
			w.RetryAt = nil
			w.State = "pending"
			if w.Paused {
				w.State = "paused"
			}
			if err := tx.Put(ctx, Bucket, persistence.Key(p.OwnerID, w.ID), w); err != nil {
				return w, err
			}
			return w, changed(ctx, tx.Store(), w)
		})
		return err
	})
	return out, err
}
func (s *Service) Delete(ctx context.Context, p core.Principal, id core.ID, m core.Mutation) error {
	if err := m.Validate(true); err != nil {
		return err
	}
	if err := s.authorize(ctx, p, id, policy.Write); err != nil {
		return err
	}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		w, err := s.Get(ctx, p, id)
		if err != nil {
			return err
		}
		if w.Version != m.ExpectedVersion {
			return core.NewError(core.Conflict, "watch changed")
		}
		if len(w.Pending) > 0 || len(w.Recent) > 0 {
			return core.NewError(core.FailedPrecondition, "watch has history; pause it instead")
		}
		w.Paused = true
		if err := tx.Delete(ctx, Bucket, persistence.Key(p.OwnerID, id)); err != nil {
			return err
		}
		return changed(ctx, tx.Store(), w)
	})
}

func changed(ctx context.Context, store persistence.Store, w Watch) error {
	if w.Paused || w.State == "completed" {
		if err := notification.Sync(ctx, store, w.OwnerID, notification.Input{Kind: "watch.failed", Source: core.Object{Kind: "watch", ID: w.ID}}, false); err != nil {
			return err
		}
	}
	return realtime.NewStore(store).Append(ctx, realtime.Visible{ID: newID(), OwnerID: w.OwnerID, Topic: "watch.changed", Object: core.Object{Kind: "watch", ID: w.ID}, Version: w.Version, CreatedAt: time.Now().UTC()})
}
