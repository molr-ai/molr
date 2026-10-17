package assistant

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	avatarutil "molr.ai/runtime/internal/avatar"
	"strconv"
	"strings"
	"sync"
	"time"

	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
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
	if err := s.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: target, Action: action}); err != nil {
		return err
	}
	if target.ID != "" {
		v, err := s.repo.Get(ctx, p.OwnerID, target.ID)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if err == nil && v.State == Cloning {
			return core.NewError(core.FailedPrecondition, "assistant clone is incomplete; retry the original clone request")
		}
	}
	return nil
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
		if err := s.authorize(ctx, p, core.Object{Kind: objectKind, ID: id}, policy.Write, ownerOnly); err != nil {
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

const objectKind = "assistant"
const bucket = "business.assistants"
const groupBucket = "business.groups"
const archiveBucket = "business.assistant.archives"
const receiptBucket = "business.assistants.receipts"

type Options struct {
	ContextWindow func(context.Context, core.Principal, core.ID) (int, error)
	CopyMemory    func(context.Context, core.Principal, core.ID, core.ID) error
	Authorizer    policy.Authorizer
	Now           func() time.Time
	NewID         func() core.ID
	Conversations conversation.Repository
	Grants        policy.Repository
	// Loop persists initialize/stop/restore in the supplied transaction context.
	Loop       func(context.Context, core.Principal, core.ID, string) error
	CheckModel func(context.Context, core.Principal, core.ID) error
	// GrantModel grants the selected provider's model access in the supplied
	// transaction. It runs after inserting the assistant, before starting its loop.
	GrantModel func(context.Context, core.Principal, Assistant) error
}
type archivedState struct {
	Grants      []policy.Grant
	ScheduleIDs []core.ID
}
type repository struct{ store persistence.Store }
type service struct {
	store   persistence.Store
	repo    Repository
	options Options
	cloneMu sync.Mutex
	clones  map[string]*cloneGate
}

func NewRepository(store persistence.Store) Repository { return &repository{store: store} }
func NewService(store persistence.Store, options Options) *service {
	if options.Conversations == nil {
		options.Conversations = conversation.NewRepository(store)
	}
	return &service{store: store, repo: NewRepository(store), options: options, clones: make(map[string]*cloneGate)}
}
func (r *repository) Get(ctx context.Context, owner, id core.ID) (Assistant, error) {
	return persistence.Load[Assistant](ctx, r.store, bucket, persistence.Key(owner, id))
}
func (r *repository) List(ctx context.Context, owner core.ID, q Query) (core.Page[Assistant], error) {
	filters := []persistence.Filter{persistence.Eq("OwnerID", owner), {Field: "State", Op: "in", Value: []State{Active, Dismissed}}}
	if q.State != "" {
		filters = append(filters, persistence.Eq("State", q.State))
	}
	if q.GroupID != "" {
		filters = append(filters, persistence.Eq("GroupID", q.GroupID))
	}

	request := q.Page
	q.Page = core.PageRequest{}
	return persistence.Page[Assistant](ctx, r.store, bucket, persistence.Query{Where: filters}, request, q, func(v Assistant) bool {
		return q.Name == "" || strings.Contains(strings.ToLower(v.Name), strings.ToLower(q.Name))
	})
}

func (r *repository) Insert(ctx context.Context, v Assistant) error {
	return insert(ctx, r.store, bucket, persistence.Key(v.OwnerID, v.ID), v)
}
func (r *repository) Save(ctx context.Context, v Assistant, expected core.Version) error {
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		old, err := r.Get(ctx, v.OwnerID, v.ID)
		if err != nil {
			return err
		}
		if err = save(ctx, r.store, bucket, persistence.Key(v.OwnerID, v.ID), v, expected, func(v Assistant) core.Version { return v.Version }); err != nil {
			return err
		}
		if old.State != v.State {
			return tx.NotifyAfterCommit(ctx, StateSignal(v.OwnerID, v.ID))
		}
		return nil
	})
}
func (r *repository) Groups(ctx context.Context, owner core.ID, p core.PageRequest) (core.Page[Group], error) {
	return persistence.Page[Group](ctx, r.store, groupBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner)}, OrderBy: "Position"}, p, owner, nil)
}

func (r *repository) PutGroup(ctx context.Context, v Group, expected core.Version) error {
	if expected == 0 {
		return insert(ctx, r.store, groupBucket, persistence.Key(v.OwnerID, v.ID), v)
	}
	return save(ctx, r.store, groupBucket, persistence.Key(v.OwnerID, v.ID), v, expected, func(v Group) core.Version { return v.Version })
}
func (r *repository) DeleteGroup(ctx context.Context, owner, id core.ID, expected core.Version) error {
	return remove[Group](ctx, r.store, groupBucket, persistence.Key(owner, id), expected, func(v Group) core.Version { return v.Version })
}
func (s *service) loop(ctx context.Context, p core.Principal, id core.ID, action string) error {
	if s.options.Loop == nil {
		return core.NewError(core.Unavailable, "agent lifecycle not configured")
	}
	return s.options.Loop(ctx, p, id, action)
}
func (s *service) model(ctx context.Context, p core.Principal, id core.ID) error {
	if id.Validate() != nil {
		return invalid("model_id")
	}
	if s.options.CheckModel == nil {
		return core.NewError(core.Unavailable, "model catalog not configured")
	}
	return s.options.CheckModel(ctx, p, id)
}
func (s *service) compressionTokens(ctx context.Context, p core.Principal, modelID core.ID, configured int) (int, error) {
	if configured < 0 {
		return 0, invalid("context_compression_tokens")
	}
	if configured == 0 {
		return 0, nil
	}
	window := core.DefaultModelContextLimit
	if s.options.ContextWindow != nil {
		var err error
		window, err = s.options.ContextWindow(ctx, p, modelID)
		if err != nil {
			return 0, err
		}
	}
	return core.CompressionTokens(window, configured), nil
}

func (s *service) Hire(ctx context.Context, p core.Principal, req HireRequest) (Assistant, error) {
	return mutate(ctx, s, p, "hire", "", req.Mutation, req, false, true, func(ctx context.Context) (Assistant, error) {
		name, err := NormalizeName(req.Name)
		if err != nil {
			return Assistant{}, err
		}
		if err := req.Budget.Validate(); err != nil {
			return Assistant{}, err
		}
		if err := s.model(ctx, p, req.ModelID); err != nil {
			return Assistant{}, err
		}
		avatar, err := avatarutil.Normalize(req.Avatar)
		if err != nil {
			return Assistant{}, err
		}
		compression, err := s.compressionTokens(ctx, p, req.ModelID, req.ContextCompressionTokens)
		if err != nil {
			return Assistant{}, err
		}
		v := Assistant{ContextCompressionTokens: compression, ID: s.id(), OwnerID: p.OwnerID, Name: name, Avatar: avatar, State: Active, ConversationID: s.id(), ModelID: req.ModelID, Budget: req.Budget, Version: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
		if err := s.repo.Insert(ctx, v); err != nil {
			return v, err
		}
		if err := s.options.Conversations.Insert(ctx, conversation.Conversation{ID: v.ConversationID, OwnerID: p.OwnerID, PeerID: v.ID, Version: 1}); err != nil {
			return v, err
		}
		if s.options.GrantModel == nil {
			return v, core.NewError(core.Unavailable, "model authorization not configured")
		}
		if err := s.options.GrantModel(ctx, p, v); err != nil {
			return v, err
		}
		if err := s.loop(ctx, p, v.ID, "initialize"); err != nil {
			return v, err
		}
		return v, event.NewWriter(s.store).AppendAssistant(ctx, event.AssistantEvent{
			ID: s.id(), OwnerID: v.OwnerID, AssistantID: v.ID,
			Kind: event.Born, DeliveryMode: event.WhenIdle, CreatedAt: v.CreatedAt,
		})
	})
}
func (s *service) Get(ctx context.Context, p core.Principal, id core.ID) (Assistant, error) {
	var result Assistant
	err := s.read(ctx, p, id, policy.Read, false, func(ctx context.Context) error {
		var err error
		result, err = s.repo.Get(ctx, p.OwnerID, id)
		return err
	})
	return result, err
}
func (s *service) List(ctx context.Context, p core.Principal, q Query) (core.Page[Assistant], error) {
	var result core.Page[Assistant]
	err := s.read(ctx, p, "", policy.Read, false, func(ctx context.Context) error {
		var err error
		result, err = s.repo.List(ctx, p.OwnerID, q)
		return err
	})
	return result, err
}
func (s *service) Update(ctx context.Context, p core.Principal, req UpdateRequest) (Assistant, error) {
	return mutate(ctx, s, p, "update", req.ID, req.Mutation, req, true, true, func(ctx context.Context) (Assistant, error) {
		v, err := s.repo.Get(ctx, p.OwnerID, req.ID)
		if err != nil {
			return v, err
		}
		if v.Version != req.Mutation.ExpectedVersion {
			return v, conflict()
		}
		name, err := NormalizeName(req.Name)
		if err != nil {
			return v, err
		}
		if err = req.Budget.Validate(); err != nil {
			return Assistant{}, err
		}
		if err = s.model(ctx, p, req.ModelID); err != nil {
			return v, err
		}
		configured := v.ContextCompressionTokens
		if req.ContextCompressionTokens != nil {
			configured = *req.ContextCompressionTokens
		}
		v.ContextCompressionTokens, err = s.compressionTokens(ctx, p, req.ModelID, configured)
		if err != nil {
			return Assistant{}, err
		}
		beforeAvatar := v.Avatar
		if req.Avatar != nil {
			v.Avatar, err = avatarutil.Normalize(*req.Avatar)
			if err != nil {
				return Assistant{}, err
			}
		}
		profileChanged := v.Name != name || (req.Avatar != nil && beforeAvatar != v.Avatar)
		v.Name = name
		v.ModelID = req.ModelID
		v.Budget = req.Budget
		v.Version++
		v.UpdatedAt = s.now()
		if err = s.repo.Save(ctx, v, req.Mutation.ExpectedVersion); err != nil {
			return Assistant{}, err
		}
		if profileChanged {
			err = s.profileChanged(ctx, v)
		}
		return v, err
	})
}
func (s *service) Place(ctx context.Context, p core.Principal, req PlacementRequest) (Assistant, error) {
	return mutate(ctx, s, p, "place", req.ID, req.Mutation, req, true, true, func(ctx context.Context) (Assistant, error) {
		v, err := s.repo.Get(ctx, p.OwnerID, req.ID)
		if err != nil {
			return v, err
		}
		if req.GroupID != "" {
			if _, err = persistence.Load[Group](ctx, s.store, groupBucket, persistence.Key(p.OwnerID, req.GroupID)); err != nil {
				return v, err
			}
		}
		v.GroupID = req.GroupID
		v.Pinned = req.Pinned
		v.Version++
		v.UpdatedAt = s.now()
		return v, s.repo.Save(ctx, v, req.Mutation.ExpectedVersion)
	})
}
func (s *service) activeGrants(ctx context.Context, owner, id core.ID) ([]policy.Grant, error) {
	if s.options.Grants == nil {
		return nil, core.NewError(core.Unavailable, "grant repository not configured")
	}
	var all []policy.Grant
	q := policy.Query{AssistantID: id, Page: core.PageRequest{Limit: 200}}
	for {
		page, err := s.options.Grants.Grants(ctx, owner, q)
		if err != nil {
			return nil, err
		}
		for _, g := range page.Items {
			if g.RevokedAt == nil && (g.ExpiresAt == nil || g.ExpiresAt.After(s.now())) {
				all = append(all, g)
			}
		}
		if page.NextCursor == "" {
			return all, nil
		}
		q.Page.Cursor = page.NextCursor
	}
}
func (s *service) copyGrants(ctx context.Context, p core.Principal, destination core.ID, selected []core.ID, available []policy.Grant) error {
	seen := map[core.ID]bool{}
	for _, id := range selected {
		if seen[id] {
			return invalid("grant_ids")
		}
		seen[id] = true
		var grant *policy.Grant
		for i := range available {
			if available[i].ID == id {
				grant = &available[i]
				break
			}
		}
		if grant == nil || grant.ExpiresAt != nil && !grant.ExpiresAt.After(s.now()) {
			return forbidden()
		}
		if err := s.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: grant.ResourceID}, ResourceID: grant.ResourceID, Action: policy.Manage}); err != nil {
			return err
		}
		copy := *grant
		copy.ID = s.id()
		copy.AssistantID = destination
		copy.Version = 1
		copy.RevokedAt = nil
		if err := s.options.Grants.InsertGrant(ctx, copy); err != nil {
			return err
		}
	}
	return nil
}
func (s *service) Dismiss(ctx context.Context, p core.Principal, req LifecycleRequest) (Assistant, error) {
	return mutate(ctx, s, p, "dismiss", req.ID, req.Mutation, req, true, true, func(ctx context.Context) (Assistant, error) {
		v, err := s.repo.Get(ctx, p.OwnerID, req.ID)
		if err != nil {
			return v, err
		}
		if v.Version != req.Mutation.ExpectedVersion {
			return v, conflict()
		}
		if v.State != Active {
			return v, core.NewError(core.FailedPrecondition, "assistant already dismissed")
		}
		grants, err := s.activeGrants(ctx, p.OwnerID, v.ID)
		if err != nil {
			return v, err
		}
		archive := archivedState{Grants: grants}
		schedules, err := persistence.Select[schedule.Schedule](ctx, s.store, "business.schedules", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID), persistence.Eq("AssistantID", v.ID), persistence.Eq("Enabled", true)}})
		if err != nil {
			return v, err
		}
		for _, plan := range schedules {
			if plan.OwnerID == p.OwnerID && plan.AssistantID == v.ID && plan.Enabled {
				archive.ScheduleIDs = append(archive.ScheduleIDs, plan.ID)
				old := plan.Version
				plan.Enabled = false
				plan.NextAt = nil
				plan.Version++
				if err = schedule.NewRepository(s.store).Save(ctx, plan, old); err != nil {
					return v, err
				}
			}
		}
		tasks, err := persistence.Select[task.Task](ctx, s.store, "business.tasks", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID), persistence.Eq("AssistantID", v.ID)}})
		if err != nil {
			return v, err
		}
		for _, work := range tasks {
			if work.OwnerID == p.OwnerID && work.AssistantID == v.ID && work.State != task.Completed && work.State != task.Canceled && work.State != task.Failed {
				old := work.Version
				work.State = task.Canceled
				work.Version++
				work.UpdatedAt = s.now()
				if err = task.NewRepository(s.store).Save(ctx, work, old); err != nil {
					return v, err
				}
			}
		}
		for _, g := range grants {
			if err = s.options.Grants.RevokeGrant(ctx, p.OwnerID, g.ID, g.Version, s.now()); err != nil {
				return v, err
			}
		}
		conversation, err := s.options.Conversations.Get(ctx, p.OwnerID, v.ConversationID)
		if err != nil {
			return v, err
		}
		if err = s.options.Conversations.SetArchived(ctx, p.OwnerID, v.ConversationID, true, conversation.Version); err != nil {
			return v, err
		}
		if err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, archiveBucket, persistence.Key(p.OwnerID, v.ID), archive)
		}); err != nil {
			return v, err
		}
		v.State = Dismissed
		v.Version++
		v.UpdatedAt = s.now()
		if err = s.repo.Save(ctx, v, req.Mutation.ExpectedVersion); err != nil {
			return v, err
		}
		return v, s.loop(ctx, p, v.ID, "stop")
	})
}
func (s *service) Rehire(ctx context.Context, p core.Principal, req RehireRequest) (Assistant, error) {
	return mutate(ctx, s, p, "rehire", req.ID, req.Mutation, req, true, true, func(ctx context.Context) (Assistant, error) {
		v, err := s.repo.Get(ctx, p.OwnerID, req.ID)
		if err != nil {
			return v, err
		}
		if v.Version != req.Mutation.ExpectedVersion {
			return v, conflict()
		}
		if v.State != Dismissed {
			return v, core.NewError(core.FailedPrecondition, "assistant is active")
		}
		archive, err := persistence.Load[archivedState](ctx, s.store, archiveBucket, persistence.Key(p.OwnerID, v.ID))
		if err != nil {
			return v, err
		}
		if err = s.copyGrants(ctx, p, v.ID, req.RestoreGrantIDs, archive.Grants); err != nil {
			return v, err
		}
		for _, id := range req.RestoreScheduleIDs {
			allowed := false
			for _, archived := range archive.ScheduleIDs {
				if id == archived {
					allowed = true
				}
			}
			if !allowed {
				return v, forbidden()
			}
			repo := schedule.NewRepository(s.store)
			plan, err := repo.Get(ctx, p.OwnerID, id)
			if err != nil {
				return v, err
			}
			if plan.AssistantID != v.ID {
				return v, forbidden()
			}
			old := plan.Version
			plan.Enabled = true
			plan.NextAt = &plan.Trigger.StartAt
			plan.Version++
			if err = repo.Save(ctx, plan, old); err != nil {
				return v, err
			}
		}
		c, err := s.options.Conversations.Get(ctx, p.OwnerID, v.ConversationID)
		if err != nil {
			return v, err
		}
		if err = s.options.Conversations.SetArchived(ctx, p.OwnerID, v.ConversationID, false, c.Version); err != nil {
			return v, err
		}
		v.State = Active
		v.Version++
		v.UpdatedAt = s.now()
		if err = s.repo.Save(ctx, v, req.Mutation.ExpectedVersion); err != nil {
			return v, err
		}
		return v, s.loop(ctx, p, v.ID, "restore")
	})
}
func (s *service) Clone(ctx context.Context, p core.Principal, req CloneRequest) (Assistant, error) {
	unlock, err := s.lockClone(ctx, persistence.Key(p.OwnerID, core.ID(string(req.SourceID)+"/"+req.Mutation.IdempotencyKey)))
	if err != nil {
		return Assistant{}, err
	}
	defer unlock()
	result, err := mutate(ctx, s, p, "clone", req.SourceID, req.Mutation, req, false, true, func(ctx context.Context) (Assistant, error) {
		source, err := s.repo.Get(ctx, p.OwnerID, req.SourceID)
		if err != nil {
			return Assistant{}, err
		}
		name, err := NormalizeName(req.Name)
		if err != nil {
			return Assistant{}, err
		}
		if err := source.Budget.Validate(); err != nil {
			return Assistant{}, core.WithReason(core.NewError(core.BudgetExceeded, "source assistant budget not configured"), core.AssistantBudgetMissing)
		}
		v := Assistant{ID: s.id(), OwnerID: p.OwnerID, Name: name, Avatar: source.Avatar, State: Active, ConversationID: s.id(), ModelID: source.ModelID, ContextCompressionTokens: source.ContextCompressionTokens, Budget: source.Budget, Version: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
		if req.CopyMemory {
			if s.options.CopyMemory == nil {
				return Assistant{}, core.NewError(core.Unavailable, "memory file copy unavailable")
			}
			v.State = Cloning
		}
		if err = s.repo.Insert(ctx, v); err != nil {
			return v, err
		}
		if err = s.options.Conversations.Insert(ctx, conversation.Conversation{ID: v.ConversationID, OwnerID: p.OwnerID, PeerID: v.ID, Version: 1}); err != nil {
			return v, err
		}
		if req.CopyMessages {
			q := conversation.MessageQuery{ConversationID: source.ConversationID, Page: core.PageRequest{Limit: 200}}
			for {
				page, err := s.options.Conversations.Messages(ctx, p.OwnerID, q)
				if err != nil {
					return v, err
				}
				for _, message := range page.Items {
					message.ID = s.id()
					message.ConversationID = v.ConversationID
					message.Sequence = 0
					if message.Sender == conversation.Assistant {
						message.SenderID = v.ID
					}
					message.Source.OperationID = ""
					if _, err = s.options.Conversations.Append(ctx, p.OwnerID, message); err != nil {
						return v, err
					}
				}
				if page.NextCursor == "" {
					break
				}
				q.Page.Cursor = page.NextCursor
			}
		}

		if len(req.GrantIDs) > 0 {
			grants, err := s.activeGrants(ctx, p.OwnerID, source.ID)
			if err != nil {
				return v, err
			}
			if err = s.copyGrants(ctx, p, v.ID, req.GrantIDs, grants); err != nil {
				return v, err
			}
		}
		if req.CopyMemory {
			chat, err := s.options.Conversations.Get(ctx, p.OwnerID, v.ConversationID)
			if err != nil {
				return v, err
			}
			if err = s.options.Conversations.SetArchived(ctx, p.OwnerID, chat.ID, true, chat.Version); err != nil {
				return v, err
			}
			err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				return tx.Put(ctx, cloneBucket, persistence.Key(p.OwnerID, v.ID), pendingClone{OwnerID: p.OwnerID, AssistantID: v.ID, Request: req})
			})
			return v, err
		}
		return v, s.loop(ctx, p, v.ID, "initialize")
	})
	if err != nil || result.State != Cloning {
		return result, err
	}
	return s.finishClone(ctx, p, req, result, true)
}

func (s *service) Groups(ctx context.Context, p core.Principal, q core.PageRequest) (core.Page[Group], error) {
	var result core.Page[Group]
	err := s.read(ctx, p, "", policy.Read, true, func(ctx context.Context) error {
		var err error
		result, err = s.repo.Groups(ctx, p.OwnerID, q)
		return err
	})
	return result, err
}
func (s *service) CreateGroup(ctx context.Context, p core.Principal, req GroupRequest) (Group, error) {
	return mutate(ctx, s, p, "create-group", "", req.Mutation, req, false, true, func(ctx context.Context) (Group, error) {
		if !validText(req.Name, 4096) || req.Position < 0 {
			return Group{}, invalid("group")
		}
		g := Group{ID: s.id(), OwnerID: p.OwnerID, Name: req.Name, Position: req.Position, Version: 1}
		return g, s.repo.PutGroup(ctx, g, 0)
	})
}
func (s *service) UpdateGroup(ctx context.Context, p core.Principal, req GroupRequest) (Group, error) {
	return mutate(ctx, s, p, "update-group", req.ID, req.Mutation, req, true, true, func(ctx context.Context) (Group, error) {
		v, err := persistence.Load[Group](ctx, s.store, groupBucket, persistence.Key(p.OwnerID, req.ID))
		if err != nil {
			return v, err
		}
		if !validText(req.Name, 4096) || req.Position < 0 {
			return v, invalid("group")
		}
		v.Name = req.Name
		v.Position = req.Position
		v.Version++
		return v, s.repo.PutGroup(ctx, v, req.Mutation.ExpectedVersion)
	})
}
func (s *service) DeleteGroup(ctx context.Context, p core.Principal, req LifecycleRequest) error {
	_, err := mutate(ctx, s, p, "delete-group", req.ID, req.Mutation, req, true, true, func(ctx context.Context) (struct{}, error) {
		if err := s.repo.DeleteGroup(ctx, p.OwnerID, req.ID, req.Mutation.ExpectedVersion); err != nil {
			return struct{}{}, err
		}
		rows, err := persistence.Select[Assistant](ctx, s.store, bucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID), persistence.Eq("GroupID", req.ID)}})
		if err != nil {
			return struct{}{}, err
		}
		for _, v := range rows {
			if v.OwnerID == p.OwnerID && v.GroupID == req.ID {
				old := v.Version
				v.GroupID = ""
				v.Version++
				if err = s.repo.Save(ctx, v, old); err != nil {
					return struct{}{}, err
				}
			}
		}
		return struct{}{}, nil
	})
	return err
}

// StateSignal wakes observers after an assistant lifecycle change commits.
func StateSignal(owner, id core.ID) persistence.SignalKey {
	return persistence.SignalKey{Namespace: "assistant.state", Key: persistence.Key(owner, id)}
}
