package memory

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

func invalid(field string) error { return core.Invalid(field, "invalid value") }
func conflict() error            { return core.NewError(core.Conflict, "version or idempotency conflict") }
func missing() error             { return core.NewError(core.NotFound, "object not found") }
func forbidden() error           { return core.NewError(core.Forbidden, "access denied") }
func (s *service) now() time.Time {
	if s.options.Now != nil {
		return s.options.Now()
	}
	return time.Now().UTC()
}
func (s *service) authorize(ctx context.Context, p core.Principal, target core.Object, action policy.Action, ownerOnly bool) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if ownerOnly {
		if err := p.RequireOwner(); err != nil {
			return err
		}
	}
	if s.options.Authorizer == nil {
		return core.NewError(core.Unavailable, "authorization not configured")
	}
	return s.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: target, Action: action})
}
func paginate[T any](items []T, owner core.ID, query any, request core.PageRequest) (core.Page[T], error) {
	var result core.Page[T]
	p, err := request.Normalize()
	if err != nil {
		return result, err
	}
	encoded, _ := json.Marshal(query)
	digest := sha256.Sum256(encoded)
	scope := string(owner) + ":" + hex.EncodeToString(digest[:]) + ":"
	offset := 0
	if p.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(p.Cursor)
		if err != nil || !strings.HasPrefix(string(raw), scope) {
			return result, invalid("cursor")
		}
		offset, err = strconv.Atoi(strings.TrimPrefix(string(raw), scope))
		if err != nil || offset < 0 || offset > len(items) {
			return result, invalid("cursor")
		}
	}
	end := offset + p.Limit
	if end > len(items) {
		end = len(items)
	}
	result.Items = items[offset:end]
	if end < len(items) {
		result.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(scope + strconv.Itoa(end)))
	}
	return result, nil
}
func validText(text string, limit int) bool {
	return strings.TrimSpace(text) != "" && len(text) <= limit
}

const objectKind = "memory"

type Options struct {
	Authorizer     policy.Authorizer
	Now            func() time.Time
	Files          *Files
	CheckAssistant func(context.Context, core.Principal, core.ID) error
}
type service struct {
	store   persistence.Store
	repo    *Files
	options Options
	mu      sync.Mutex
}

func NewService(store persistence.Store, options Options) Service {
	return &service{store: store, repo: options.Files, options: options}
}
func (s *service) checkAssistant(ctx context.Context, p core.Principal, id core.ID) error {
	if id == "" {
		return nil
	}
	if s.options.CheckAssistant == nil {
		return core.NewError(core.Unavailable, "assistant verification not configured")
	}
	return s.options.CheckAssistant(ctx, p, id)
}

func (s *service) access(ctx context.Context, p core.Principal, ref Ref, write, directory bool) error {
	if err := ref.Validate(directory); err != nil {
		return err
	}
	action := policy.Read
	if write {
		action = policy.Write
	}
	if err := s.authorize(ctx, p, core.Object{Kind: objectKind}, action, false); err != nil {
		return err
	}
	if p.Kind == core.ActorAssistant {
		if ref.Scope == Assistant && ref.AssistantID != p.SubjectID {
			return forbidden()
		}
		if ref.Scope == Global && write {
			return forbidden()
		}
	}
	if write && ref.Scope == Assistant {
		return s.checkAssistant(ctx, p, ref.AssistantID)
	}
	return nil
}
func (s *service) Create(ctx context.Context, p core.Principal, req CreateRequest) (Memory, error) {
	return mutate(ctx, s, p, "create", req.Ref, req.Mutation, req, false, false, func(ctx context.Context) (Memory, error) {
		if err := s.access(ctx, p, req.Ref, true, false); err != nil {
			return Memory{}, err
		}
		v := Memory{Ref: req.Ref, OwnerID: p.OwnerID, State: Active, Context: req.Context, Confirmed: p.Kind == core.ActorOwner, Text: req.Text, Sources: req.Sources, CreatedAt: s.now(), UpdatedAt: s.now()}
		return s.repo.Put(ctx, v, "", func() error { return s.access(ctx, p, req.Ref, true, false) })
	})
}
func (s *service) Get(ctx context.Context, p core.Principal, ref Ref) (Memory, error) {
	if err := s.access(ctx, p, ref, false, false); err != nil {
		return Memory{}, err
	}
	return s.repo.Get(ctx, p.OwnerID, ref)
}
func (s *service) List(ctx context.Context, p core.Principal, q Query) (core.Page[Memory], error) {
	if err := s.authorize(ctx, p, core.Object{Kind: objectKind}, policy.Read, false); err != nil {
		return core.Page[Memory]{}, err
	}
	if p.Kind == core.ActorAssistant && q.Scope != Global {
		if q.AssistantID != "" && q.AssistantID != p.SubjectID {
			return core.Page[Memory]{}, forbidden()
		}
		q.Scope = Assistant
		q.AssistantID = p.SubjectID
	}
	return s.repo.List(ctx, p.OwnerID, q)
}
func (s *service) Update(ctx context.Context, p core.Principal, req UpdateRequest) (Memory, error) {
	return mutate(ctx, s, p, "update", req.Ref, req.Mutation, req, true, false, func(ctx context.Context) (Memory, error) {
		if err := s.access(ctx, p, req.Ref, true, false); err != nil {
			return Memory{}, err
		}
		v, err := s.repo.Get(ctx, p.OwnerID, req.Ref)
		if err != nil {
			return v, err
		}
		if v.Version != req.Mutation.ExpectedVersion {
			return v, conflict()
		}
		if req.Context != nil {
			v.Context = *req.Context
		}
		if req.State != nil {
			if p.Kind != core.ActorOwner || (*req.State != Active && *req.State != Pending && *req.State != Superseded) {
				return v, invalid("state")
			}
			v.State = *req.State
		}
		if p.Kind == core.ActorOwner {
			v.Confirmed = true
			// Owner-maintained cognition no longer follows automatic dependencies.
			v.Dependencies = nil
		}
		if req.ReplacesVersion != nil {
			if p.Kind != core.ActorOwner {
				return v, forbidden()
			}
			v.ReplacesVersion = *req.ReplacesVersion
		}
		if req.State != nil && *req.State == Active && v.Replaces != nil {
			old, err := s.Get(ctx, p, *v.Replaces)
			if err != nil {
				return v, err
			}
			if old.Version != v.ReplacesVersion || old.State != Superseded {
				return v, conflict()
			}
			v.Replaces = nil
			v.ReplacesVersion = ""
		}
		v.Text = req.Text
		v.Sources = req.Sources
		v.UpdatedAt = s.now()
		return s.repo.Put(ctx, v, req.Mutation.ExpectedVersion, func() error { return s.access(ctx, p, req.Ref, true, false) })
	})
}
func (s *service) Delete(ctx context.Context, p core.Principal, req DeleteRequest) error {
	action := "delete"
	if req.Forget {
		action = "forget"
	}
	_, err := mutate(ctx, s, p, action, req.Ref, req.Mutation, req, true, false, func(ctx context.Context) (struct{}, error) {
		if err := s.access(ctx, p, req.Ref, true, false); err != nil {
			return struct{}{}, err
		}
		v, err := s.repo.Get(ctx, p.OwnerID, req.Ref)
		if err != nil {
			return struct{}{}, err
		}
		if err = s.repo.Delete(ctx, v, req.Mutation.ExpectedVersion, req.Forget, func() error { return s.access(ctx, p, req.Ref, true, false) }); err != nil {
			return struct{}{}, err
		}
		if req.Forget {
			return struct{}{}, s.scrub(ctx, p, v)
		}
		return struct{}{}, nil
	})
	return err
}
