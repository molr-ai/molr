package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

// Cloning is internal staging state, resumed by startup or the original request.
// It is excluded from assistant lists and cannot be used by lifecycle commands.
const Cloning State = "cloning"
const cloneBucket = "business.assistants.clones"

type pendingClone struct {
	OwnerID, AssistantID core.ID
	Request              CloneRequest
}

// RecoverClones is a trusted startup port. Resume only operations that were
// authorized and staged atomically; no client session is retained or replayed.
// Run before serving HTTP or scheduling work. Failed copies remain staged.
func (s *service) RecoverClones(ctx context.Context) error {
	var failures []error
	query := persistence.Query{Limit: 100}
	for {
		var rows []persistence.Record
		err := s.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
			var err error
			rows, err = tx.Query(ctx, cloneBucket, query)
			return err
		})
		if err != nil {
			return errors.Join(append(failures, err)...)
		}
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return errors.Join(append(failures, err)...)
			}
			operation, err := persistence.Decode[pendingClone](row)
			if err == nil {
				err = s.recoverClone(ctx, operation)
			}
			if err != nil {
				failures = append(failures, fmt.Errorf("clone %s: %w", row.Key, err))
			}
		}
		if len(rows) < query.Limit {
			return errors.Join(failures...)
		}
		query.After = []any{rows[len(rows)-1].Key}
	}
}

func (s *service) recoverClone(ctx context.Context, operation pendingClone) error {
	p := core.Principal{OwnerID: operation.OwnerID, SubjectID: operation.OwnerID, Kind: core.ActorOwner, SessionID: "clone-recovery"}
	v, err := s.repo.Get(ctx, p.OwnerID, operation.AssistantID)
	if err != nil {
		return err
	}
	if v.State != Cloning {
		return conflict()
	}
	_, err = s.finishClone(ctx, p, operation.Request, v, false)
	return err
}

type cloneGate struct {
	done  chan struct{}
	users int
}

// The Runtime owns its database exclusively. Serialize each request's file
// journal within that instance without holding a database transaction during I/O.
func (s *service) lockClone(ctx context.Context, key string) (func(), error) {
	s.cloneMu.Lock()
	gate := s.clones[key]
	if gate == nil {
		gate = &cloneGate{done: make(chan struct{}, 1)}
		s.clones[key] = gate
	}
	gate.users++
	s.cloneMu.Unlock()
	release := func() {
		s.cloneMu.Lock()
		defer s.cloneMu.Unlock()
		gate.users--
		if gate.users == 0 {
			delete(s.clones, key)
		}
	}
	select {
	case gate.done <- struct{}{}:
		return func() { <-gate.done; release() }, nil
	case <-ctx.Done():
		release()
		return nil, ctx.Err()
	}
}

func (s *service) finishClone(ctx context.Context, p core.Principal, req CloneRequest, pending Assistant, authorize bool) (Assistant, error) {
	if s.options.CopyMemory == nil {
		return Assistant{}, core.NewError(core.Unavailable, "memory file copy unavailable")
	}
	if err := s.options.CopyMemory(ctx, p, req.SourceID, pending.ID); err != nil {
		return Assistant{}, err
	}
	var result Assistant
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		// Authorization may have changed while files were copied.
		if authorize {
			if err := s.authorize(ctx, p, core.Object{Kind: objectKind, ID: req.SourceID}, policy.Write, true); err != nil {
				return err
			}
		} else {
			source, err := s.repo.Get(ctx, p.OwnerID, req.SourceID)
			if err != nil {
				return err
			}
			if source.OwnerID != p.OwnerID {
				return forbidden()
			}
		}
		key := persistence.Key(p.OwnerID, core.ID("clone/"+string(req.SourceID)+"/"+req.Mutation.IdempotencyKey))
		var saved receipt
		if err := tx.Get(ctx, receiptBucket, key, &saved); err != nil {
			return err
		}
		if err := json.Unmarshal(saved.Result, &result); err != nil {
			return err
		}
		if result.State != Cloning {
			return nil
		}
		v, err := s.repo.Get(ctx, p.OwnerID, pending.ID)
		if err != nil {
			return err
		}
		if v.State != Cloning {
			return conflict()
		}
		chat, err := s.options.Conversations.Get(ctx, p.OwnerID, v.ConversationID)
		if err != nil {
			return err
		}
		if err = s.options.Conversations.SetArchived(ctx, p.OwnerID, chat.ID, false, chat.Version); err != nil {
			return err
		}
		if err = s.loop(ctx, p, v.ID, "initialize"); err != nil {
			return err
		}
		old := v.Version
		v.State = Active
		v.Version++
		v.UpdatedAt = s.now()
		if err = s.repo.Save(ctx, v, old); err != nil {
			return err
		}
		saved.Result, err = json.Marshal(v)
		if err != nil {
			return err
		}
		result = v
		if err = tx.Put(ctx, receiptBucket, key, saved); err != nil {
			return err
		}
		return tx.Delete(ctx, cloneBucket, persistence.Key(p.OwnerID, v.ID))
	})
	if err != nil {
		return Assistant{}, err
	}
	return result, nil
}
