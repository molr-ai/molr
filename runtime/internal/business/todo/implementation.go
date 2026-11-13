package todo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"molr.ai/runtime/internal/business/planstate"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

func newID() core.ID {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return core.ID(hex.EncodeToString(b[:]))
}
func invalid(field string) error { return core.Invalid(field, "invalid value") }
func conflict() error            { return core.NewError(core.Conflict, "version or idempotency conflict") }
func forbidden() error           { return core.NewError(core.Forbidden, "access denied") }
func (s *service) now() time.Time {
	if s.options.Now != nil {
		return s.options.Now()
	}
	return time.Now().UTC()
}
func (s *service) id() core.ID {
	if s.options.NewID != nil {
		return s.options.NewID()
	}
	return newID()
}
func (s *service) authorize(ctx context.Context, p core.Principal, target core.Object, action policy.Action, ownerOnly bool) error {
	return policy.Authorize(ctx, p, s.options.Authorizer, policy.CheckRequest{Target: target, Action: action}, ownerOnly)
}
func (s *service) read(ctx context.Context, p core.Principal, id core.ID, action policy.Action, ownerOnly bool, fn func(context.Context) error) error {
	return s.store.Read(ctx, func(ctx context.Context, _ persistence.Tx) error {
		if err := s.authorize(ctx, p, core.Object{Kind: objectKind, ID: id}, action, ownerOnly); err != nil {
			return err
		}
		return fn(ctx)
	})
}

func mutate[T any](ctx context.Context, s *service, p core.Principal, action string, id core.ID, m core.Mutation, payload any, update, ownerOnly bool, fn func(context.Context) (T, error)) (T, error) {
	var result T
	if err := m.Validate(update); err != nil {
		return result, err
	}
	digest, err := persistence.PayloadDigest(payload)
	if err != nil {
		return result, err
	}
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := s.authorize(ctx, p, core.Object{Kind: objectKind, ID: id}, policy.Write, ownerOnly); err != nil {
			return err
		}
		key := persistence.Key(p.OwnerID, core.ID(action+"/"+string(id)+"/"+m.IdempotencyKey))
		result, err = persistence.MutateReceipt(ctx, tx, receiptBucket, key, digest, fn)
		return err
	})
	return result, err
}
func validText(text string, limit int) bool {
	return strings.TrimSpace(text) != "" && len(text) <= limit
}

const objectKind = "todo"
const bucket = "business.todos"
const receiptBucket = "business.todos.receipts"

type Options struct {
	Authorizer policy.Authorizer
	Now        func() time.Time
	NewID      func() core.ID
	// CheckAssistant checks current ownership and lifecycle, using the transaction context.
	CheckAssistant func(context.Context, core.Principal, core.ID) error
}
type repository struct{ store persistence.Store }
type service struct {
	store   persistence.Store
	repo    Repository
	options Options
}

func NewRepository(store persistence.Store) Repository { return &repository{store: store} }
func NewService(store persistence.Store, options Options) Service {
	return &service{store: store, repo: NewRepository(store), options: options}
}
func (r *repository) Get(ctx context.Context, owner, id core.ID) (Todo, error) {
	v, err := persistence.Load[Todo](ctx, r.store, bucket, persistence.Key(owner, id))
	if err == nil {
		err = planstate.Visible(v.State)
	}
	return v, err
}
func (r *repository) List(ctx context.Context, owner core.ID, q Query) (core.Page[Todo], error) {
	filters := []persistence.Filter{persistence.Eq("OwnerID", owner), {Field: "State", Op: "!=", Value: planstate.Deleted}}
	if q.AssistantID != "" {
		filters = append(filters, persistence.Eq("AssistantID", q.AssistantID))
	}
	if q.Completed != nil {
		filters = append(filters, persistence.Eq("Completed", *q.Completed))
	}
	request := q.Page
	q.Page = core.PageRequest{}
	return persistence.Page[Todo](ctx, r.store, bucket, persistence.Query{Where: filters}, request, q, func(v Todo) bool { return true })
}

func (r *repository) Insert(ctx context.Context, v Todo) error {
	if v.State == "" {
		v.State = planstate.Active
	}
	return persistence.Insert(ctx, r.store, bucket, persistence.Key(v.OwnerID, v.ID), v)
}
func (r *repository) Save(ctx context.Context, v Todo, expected core.Version) error {
	return persistence.SaveVersion(ctx, r.store, bucket, persistence.Key(v.OwnerID, v.ID), v, expected, func(v Todo) core.Version { return v.Version })
}
func (r *repository) Delete(ctx context.Context, owner, id core.ID, expected core.Version) error {
	return planstate.Delete(ctx, r.store, owner, core.Object{Kind: "todo", ID: id}, expected)
}
func (s *service) checkAssistant(ctx context.Context, p core.Principal, id core.ID) error {
	if p.Kind == core.ActorAssistant && id != p.SubjectID {
		return forbidden()
	}
	if id == "" {
		return nil
	}
	if s.options.CheckAssistant == nil {
		return core.NewError(core.Unavailable, "assistant verification not configured")
	}
	return s.options.CheckAssistant(ctx, p, id)
}
func (s *service) Create(ctx context.Context, p core.Principal, req CreateRequest) (Todo, error) {
	return mutate(ctx, s, p, "create", "", req.Mutation, req, false, false, func(ctx context.Context) (Todo, error) {
		if !validText(req.Title, 4096) || !validTimeBlock(req.StartAt, req.EndAt) {
			return Todo{}, invalid("request")
		}
		if err := s.checkAssistant(ctx, p, req.AssistantID); err != nil {
			return Todo{}, err
		}
		v := Todo{State: planstate.Active, ID: s.id(), OwnerID: p.OwnerID, Version: 1, Title: req.Title, Notes: req.Notes, AssistantID: req.AssistantID, DueAt: req.DueAt, StartAt: req.StartAt, EndAt: req.EndAt}
		return v, s.repo.Insert(ctx, v)
	})
}
func (s *service) Get(ctx context.Context, p core.Principal, id core.ID) (Todo, error) {
	var v Todo
	err := s.read(ctx, p, id, policy.Read, false, func(ctx context.Context) error {
		var err error
		v, err = s.repo.Get(ctx, p.OwnerID, id)
		if err != nil {
			return err
		}
		return nil
	})
	return v, err
}
func (s *service) List(ctx context.Context, p core.Principal, q Query) (core.Page[Todo], error) {
	var result core.Page[Todo]
	err := s.read(ctx, p, "", policy.Read, false, func(ctx context.Context) error {
		var err error
		if p.Kind == core.ActorAssistant {
			if q.AssistantID != "" && q.AssistantID != p.SubjectID {
				return forbidden()
			}
			q.AssistantID = p.SubjectID
		}
		result, err = s.repo.List(ctx, p.OwnerID, q)
		return err
	})
	return result, err
}
func (s *service) Update(ctx context.Context, p core.Principal, req UpdateRequest) (Todo, error) {
	return mutate(ctx, s, p, "update", req.ID, req.Mutation, req, true, false, func(ctx context.Context) (Todo, error) {
		v, err := s.repo.Get(ctx, p.OwnerID, req.ID)
		if err != nil {
			return v, err
		}
		if v.Version != req.Mutation.ExpectedVersion {
			return v, conflict()
		}
		if !validText(req.Title, 4096) || !validTimeBlock(req.StartAt, req.EndAt) {
			return v, invalid("request")
		}
		if err := s.checkAssistant(ctx, p, req.AssistantID); err != nil {
			return v, err
		}
		v.Title = req.Title
		v.Notes = req.Notes
		v.AssistantID = req.AssistantID
		v.DueAt = req.DueAt
		v.StartAt = req.StartAt
		v.EndAt = req.EndAt
		v.Completed = req.Completed
		v.Version++
		return v, s.repo.Save(ctx, v, req.Mutation.ExpectedVersion)
	})
}
func (s *service) Delete(ctx context.Context, p core.Principal, req DeleteRequest) error {
	_, err := mutate(ctx, s, p, "delete", req.ID, req.Mutation, req, true, false, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, s.repo.Delete(ctx, p.OwnerID, req.ID, req.Mutation.ExpectedVersion)
	})
	return err
}

func validTimeBlock(start, end *time.Time) bool {
	if start == nil && end == nil {
		return true
	}
	return start != nil && end != nil && !start.IsZero() && end.After(*start)
}
