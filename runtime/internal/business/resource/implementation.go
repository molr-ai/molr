package resource

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"molr.ai/runtime/internal/catalog"
	"strconv"
	"strings"
	"time"

	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
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
func missing() error             { return core.NewError(core.NotFound, "object not found") }
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
func (s *service) read(ctx context.Context, p core.Principal, id core.ID, action policy.Action, ownerOnly bool, fn func(context.Context) error) error {
	return s.store.Read(ctx, func(ctx context.Context, _ persistence.Tx) error {
		if err := s.authorize(ctx, p, core.Object{Kind: objectKind, ID: id}, action, ownerOnly); err != nil {
			return err
		}
		return fn(ctx)
	})
}

type receipt struct {
	Digest string
	Result json.RawMessage
}

func mutate[T any](ctx context.Context, s *service, p core.Principal, action string, id core.ID, m core.Mutation, payload any, update, ownerOnly bool, fn func(context.Context) (T, error)) (T, error) {
	return mutateForTarget(ctx, s, p, action, id, m, payload, update, ownerOnly, core.Object{Kind: objectKind, ID: id}, fn)
}
func mutateForTarget[T any](ctx context.Context, s *service, p core.Principal, action string, id core.ID, m core.Mutation, payload any, update, ownerOnly bool, target core.Object, fn func(context.Context) (T, error)) (T, error) {
	var result T
	if err := m.Validate(update); err != nil {
		return result, err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return result, err
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := s.authorize(ctx, p, target, policy.Write, ownerOnly); err != nil {
			return err
		}
		key := persistence.Key(p.OwnerID, core.ID(action+"/"+string(id)+"/"+m.IdempotencyKey))
		var old receipt
		err := tx.Get(ctx, receiptBucket, key, &old)
		if err == nil {
			if old.Digest != digest {
				return conflict()
			}
			return json.Unmarshal(old.Result, &result)
		}
		if !persistence.Missing(err) {
			return err
		}
		result, err = fn(ctx)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return err
		}
		target := core.Object{Kind: objectKind, ID: id}
		if value, ok := any(result).(Resource); ok {
			target.ID = value.ID
		}
		if err := audit.NewRepository(tx.Store()).Append(ctx, audit.Record{ID: s.id(), OwnerID: p.OwnerID, SubjectID: p.SubjectID, ActorKind: p.Kind, Action: "resource." + action, Target: target, Outcome: audit.Succeeded, At: s.now()}); err != nil {
			return err
		}
		return tx.Put(ctx, receiptBucket, key, receipt{Digest: digest, Result: encoded})
	})
	return result, err
}
func insert[T any](ctx context.Context, store persistence.Store, bucket, key string, value T) error {
	return store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var old json.RawMessage
		err := tx.Get(ctx, bucket, key, &old)
		if err == nil {
			return conflict()
		}
		if !persistence.Missing(err) {
			return err
		}
		return tx.Put(ctx, bucket, key, value)
	})
}
func save[T any](ctx context.Context, store persistence.Store, bucket, key string, value T, expected core.Version, version func(T) core.Version) error {
	return store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var old T
		if err := tx.Get(ctx, bucket, key, &old); err != nil {
			return err
		}
		if expected == 0 || version(old) != expected || version(value) != expected+1 {
			return conflict()
		}
		return tx.Put(ctx, bucket, key, value)
	})
}
func remove[T any](ctx context.Context, store persistence.Store, bucket, key string, expected core.Version, version func(T) core.Version) error {
	return store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var old T
		if err := tx.Get(ctx, bucket, key, &old); err != nil {
			return err
		}
		if expected == 0 || version(old) != expected {
			return conflict()
		}
		return tx.Delete(ctx, bucket, key)
	})
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

const objectKind = "resource"
const bucket = "business.resources"
const bindingBucket = "business.bindings"
const receiptBucket = "business.resources.receipts"

// Provider is a fixed deployment-configured adapter, not user-uploaded code.
// Validate must not perform external effects. Check runs outside transactions.
type Provider struct {
	Test     func(context.Context, core.Principal, TestConnectionRequest) (ConnectionTest, error)
	Kind     Kind
	Validate func(context.Context, Binding) ([]string, error)
	Check    func(context.Context, Binding) (Status, error)
	Discover func(context.Context, core.Principal, Binding) ([]ModelConfig, error)
}
type Options struct {
	Credentials       credential.Manager
	CheckVerification func(context.Context, core.Principal, auth.Verification, string) error
	// OnDeleteProvider updates cross-module references within the current transaction.
	OnDeleteProvider func(context.Context, core.Principal, Resource, Binding) error
	Authorizer       policy.Authorizer
	Now              func() time.Time
	NewID            func() core.ID
	Providers        map[string]Provider
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

func (s *service) TestConnection(ctx context.Context, p core.Principal, req TestConnectionRequest) (ConnectionTest, error) {
	var result ConnectionTest
	if err := s.read(ctx, p, "", policy.Manage, true, func(context.Context) error { return nil }); err != nil {
		return result, err
	}
	provider, ok := s.options.Providers[req.Provider]
	if req.Binding.ChatGPT != nil || !ok || provider.Test == nil || provider.Validate == nil || req.Binding.ResourceID != "" || len(req.Binding.Models) != 0 || (!req.APIKey.Empty() && req.Binding.CredentialID != "") {
		return result, invalid("connection")
	}
	if p.SessionID == "" {
		return result, forbidden()
	}
	if _, err := provider.Validate(ctx, req.Binding); err != nil {
		return result, err
	}
	return provider.Test(ctx, p, req)
}
func (r *repository) Get(ctx context.Context, owner, id core.ID) (Resource, error) {
	return catalog.Load[Resource](ctx, r.store, bucket, id)
}
func (r *repository) List(ctx context.Context, owner core.ID, q Query) (core.Page[Resource], error) {
	owner = catalog.Scope
	filters := []persistence.Filter{persistence.Eq("OwnerID", owner)}
	if q.Kind != "" {
		filters = append(filters, persistence.Eq("Kind", q.Kind))
	}

	request := q.Page
	q.Page = core.PageRequest{}
	return persistence.Page[Resource](ctx, r.store, bucket, persistence.Query{Where: filters}, request, q, func(v Resource) bool {
		return q.Name == "" || strings.Contains(strings.ToLower(v.Name), strings.ToLower(q.Name))
	})
}

func (r *repository) Insert(ctx context.Context, v Resource, b Binding) error {
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		v.OwnerID = catalog.Scope
		if b.ResourceID != v.ID {
			return invalid("resource_id")
		}
		if err := insert(ctx, r.store, bucket, persistence.Key(v.OwnerID, v.ID), v); err != nil {
			return err
		}
		return putBinding(ctx, tx, v.OwnerID, b)
	})
}
func (r *repository) Save(ctx context.Context, v Resource, expected core.Version) error {
	v.OwnerID = catalog.Scope
	return save(ctx, r.store, bucket, persistence.Key(v.OwnerID, v.ID), v, expected, func(v Resource) core.Version { return v.Version })
}
func (r *repository) Binding(ctx context.Context, owner, id core.ID) (Binding, error) {
	return catalog.Load[Binding](ctx, r.store, bindingBucket, id)
}
func (s *service) Connect(ctx context.Context, p core.Principal, req ConnectRequest) (Resource, error) {
	return mutate(ctx, s, p, "connect", "", req.Mutation, req, false, true, func(ctx context.Context) (Resource, error) {
		if !validText(req.Name, 4096) || req.Binding.ResourceID != "" || req.Binding.ChatGPT != nil {
			return Resource{}, invalid("request")
		}
		provider, ok := s.options.Providers[req.Provider]
		if !ok || provider.Kind != req.Kind || provider.Validate == nil {
			return Resource{}, invalid("provider")
		}
		for _, m := range req.Binding.Models {
			if m.Discovered {
				return Resource{}, invalid("model_source")
			}
		}
		actions, err := provider.Validate(ctx, req.Binding)
		if err != nil {
			return Resource{}, err
		}
		v := Resource{ID: s.id(), OwnerID: catalog.Scope, Kind: req.Kind, Name: req.Name, Provider: req.Provider, Actions: actions, State: Available, Version: 1}
		b := req.Binding
		if req.Kind == Storage || req.Kind == Compute || req.Kind == Network {
			v.SandboxID = b.SandboxID
			if v.SandboxID == "" {
				v.SandboxID = "local"
			}
		}
		b.ResourceID = v.ID
		return v, s.repo.Insert(ctx, v, b)
	})
}

func (s *service) RefreshModels(ctx context.Context, p core.Principal, req RefreshModelsRequest) (Resource, error) {
	if err := req.Mutation.Validate(true); err != nil {
		return Resource{}, err
	}
	var current Resource
	var binding Binding
	if err := s.read(ctx, p, req.ID, policy.Manage, true, func(ctx context.Context) error {
		var err error
		current, err = s.repo.Get(ctx, p.OwnerID, req.ID)
		if err != nil {
			return err
		}
		if current.State != Available || current.Kind != Model {
			return core.NewError(core.FailedPrecondition, "provider unavailable")
		}
		binding, err = s.repo.Binding(ctx, p.OwnerID, req.ID)
		return err
	}); err != nil {
		return Resource{}, err
	}
	provider := s.options.Providers[current.Provider]
	if provider.Discover == nil {
		return Resource{}, core.NewError(core.FailedPrecondition, "model discovery unavailable")
	}
	// Provider IO must never hold a storage transaction.
	models, err := provider.Discover(ctx, p, binding)
	if err != nil {
		return Resource{}, err
	}
	return mutate(ctx, s, p, "refresh-models", req.ID, req.Mutation, req, true, true, func(ctx context.Context) (Resource, error) {
		v, err := s.repo.Get(ctx, p.OwnerID, req.ID)
		if err != nil {
			return v, err
		}
		if v.Version != current.Version {
			return v, conflict()
		}
		if v.State != Available {
			return v, core.NewError(core.FailedPrecondition, "provider disconnected")
		}
		merged := append([]ModelConfig{}, models...)
		for _, manual := range binding.Models {
			if manual.Discovered {
				continue
			}
			found := false
			for i := range merged {
				if merged[i].Name == manual.Name {
					manual.ProviderContextLimit = merged[i].ProviderContextLimit
					manual.ProviderOutputLimit = merged[i].ProviderOutputLimit
					manual.ProviderPricing = merged[i].ProviderPricing
					merged[i] = manual
					found = true
					break
				}
			}
			if !found {
				manual.ProviderPricing = nil
				merged = append(merged, manual)
			}
		}
		binding.Models = merged
		if _, err := provider.Validate(ctx, binding); err != nil {
			return v, err
		}
		v.Version++
		if err := s.repo.Save(ctx, v, req.Mutation.ExpectedVersion); err != nil {
			return v, err
		}
		err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return putBinding(ctx, tx, v.OwnerID, binding)
		})
		return v, err
	})
}
func (s *service) ConfigureModel(ctx context.Context, p core.Principal, req ConfigureModelRequest) (Resource, error) {
	return mutate(ctx, s, p, "configure-model", req.ID, req.Mutation, req, true, true, func(ctx context.Context) (Resource, error) {
		v, err := s.repo.Get(ctx, p.OwnerID, req.ID)
		if err != nil {
			return v, err
		}
		if v.Kind != Model || v.State != Available || req.Model.Discovered {
			return v, invalid("model")
		}
		binding, err := s.repo.Binding(ctx, p.OwnerID, req.ID)
		if err != nil {
			return v, err
		}
		req.Model.ProviderContextLimit = 0 // Catalog metadata cannot be set through the owner form.
		req.Model.ProviderOutputLimit = 0
		req.Model.ProviderPricing = nil
		found := false
		for i := range binding.Models {
			if binding.Models[i].Name == req.Model.Name {
				previous := binding.Models[i]
				req.Model.ProviderContextLimit = previous.ProviderContextLimit
				req.Model.ProviderOutputLimit = previous.ProviderOutputLimit
				req.Model.ProviderPricing = previous.ProviderPricing
				binding.Models[i] = req.Model
				found = true
				break
			}
		}
		if !found {
			binding.Models = append(binding.Models, req.Model)
		}
		provider := s.options.Providers[v.Provider]
		if provider.Validate == nil {
			return v, invalid("provider")
		}
		if _, err = provider.Validate(ctx, binding); err != nil {
			return v, err
		}
		v.Version++
		if err = s.repo.Save(ctx, v, req.Mutation.ExpectedVersion); err != nil {
			return v, err
		}
		err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return putBinding(ctx, tx, v.OwnerID, binding)
		})
		return v, err
	})
}
func (s *service) Get(ctx context.Context, p core.Principal, id core.ID) (Resource, error) {
	var result Resource
	err := s.read(ctx, p, id, policy.Read, false, func(ctx context.Context) error {
		var err error
		result, err = s.repo.Get(ctx, p.OwnerID, id)
		return err
	})
	return result, err
}
func (s *service) List(ctx context.Context, p core.Principal, q Query) (core.Page[Resource], error) {
	var result core.Page[Resource]
	err := s.read(ctx, p, "", policy.Read, false, func(ctx context.Context) error {
		var err error
		result, err = s.repo.List(ctx, p.OwnerID, q)
		if err != nil {
			return err
		}
		{
			authorized := make([]Resource, 0, len(result.Items))
			for _, item := range result.Items {
				err = s.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: item.ID}, ResourceID: item.ID, Action: policy.Read})
				if err != nil {
					if core.CodeOf(err) == core.Forbidden || core.CodeOf(err) == core.NotFound {
						continue
					}
					return err
				}
				authorized = append(authorized, item)
			}
			result.Items = authorized
		}
		return nil
	})
	return result, err
}
func (s *service) Update(ctx context.Context, p core.Principal, req UpdateRequest) (Resource, error) {
	return mutate(ctx, s, p, "update", req.ID, req.Mutation, req, true, true, func(ctx context.Context) (Resource, error) {
		v, err := s.repo.Get(ctx, p.OwnerID, req.ID)
		if err != nil {
			return v, err
		}
		if !validText(req.Name, 4096) {
			return v, invalid("name")
		}
		if v.Kind == Connection || v.Provider == "mcp" {
			return v, core.NewError(core.Forbidden, "use the service connection settings")
		}
		v.Name = req.Name
		v.Version++
		return v, s.repo.Save(ctx, v, req.Mutation.ExpectedVersion)
	})
}
func (s *service) Disconnect(ctx context.Context, p core.Principal, req DisconnectRequest) error {
	_, err := mutate(ctx, s, p, "disconnect", req.ID, req.Mutation, req, true, true, func(ctx context.Context) (struct{}, error) {
		v, err := s.repo.Get(ctx, p.OwnerID, req.ID)
		if err != nil {
			return struct{}{}, err
		}
		if v.Kind == Connection || v.Provider == "mcp" {
			return struct{}{}, core.NewError(core.Forbidden, "use the service connection settings")
		}
		binding, err := s.repo.Binding(ctx, p.OwnerID, v.ID)
		if err != nil {
			return struct{}{}, err
		}
		if binding.ChatGPT != nil {
			return struct{}{}, core.Invalid("chatgpt", "disconnect from ChatGPT connection settings")
		}

		v.State = Disconnected
		v.Version++
		return struct{}{}, s.repo.Save(ctx, v, req.Mutation.ExpectedVersion)
	})
	return err
}
func (s *service) Check(ctx context.Context, p core.Principal, id core.ID) (Status, error) {
	var v Resource
	var binding Binding
	err := s.read(ctx, p, id, policy.Manage, true, func(ctx context.Context) error {
		var err error
		v, err = s.repo.Get(ctx, p.OwnerID, id)
		if err != nil {
			return err
		}
		binding, err = s.repo.Binding(ctx, p.OwnerID, id)
		return err
	})
	if err != nil {
		return Status{}, err
	}
	if v.State == Disconnected {
		return Status{State: Disconnected, CheckedAt: s.now()}, nil
	}
	provider, ok := s.options.Providers[v.Provider]
	if !ok || provider.Check == nil {
		return Status{}, core.NewError(core.Unavailable, "provider not configured")
	}
	status, err := provider.Check(ctx, binding)
	if err != nil {
		return Status{}, err
	}
	status.CheckedAt = s.now()
	return status, nil
}
