package task

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"molr.ai/runtime/internal/notification"
	"strconv"
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

const objectKind = "task"
const bucket = "business.tasks"
const revisionBucket = "business.revisions"
const receiptBucket = "business.tasks.receipts"

type Options struct {
	BindPlan          func(context.Context, core.Principal, *Task, CreateRequest) error
	Authorizer        policy.Authorizer
	Now               func() time.Time
	NewID             func() core.ID
	CheckAssistant    func(context.Context, core.Principal, core.ID) error
	ValidateExecution func(context.Context, core.Execution) error
	VerifyEvidence    func(context.Context, core.Execution, Delivery) error
	// Emit persists the execution event in this same transaction; no external work.
	Emit func(context.Context, core.Principal, Task, string) error
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
func NewExecution(store persistence.Store, options Options) Execution {
	return &service{store: store, repo: NewRepository(store), options: options}
}
func (r *repository) Get(ctx context.Context, owner, id core.ID) (Task, error) {
	return persistence.Load[Task](ctx, r.store, bucket, persistence.Key(owner, id))
}
func (r *repository) List(ctx context.Context, owner core.ID, q Query) (core.Page[Task], error) {
	filters := []persistence.Filter{persistence.Eq("OwnerID", owner)}
	if q.AssistantID != "" {
		filters = append(filters, persistence.Eq("AssistantID", q.AssistantID))
	}
	if len(q.States) > 0 {
		filters = append(filters, persistence.Filter{Field: "State", Op: "in", Value: q.States})
	}
	request := q.Page
	q.Page = core.PageRequest{}
	return persistence.Page[Task](ctx, r.store, bucket, persistence.Query{Where: filters}, request, q, func(v Task) bool { return true })
}

func (r *repository) Insert(ctx context.Context, v Task) error {
	return insert(ctx, r.store, bucket, persistence.Key(v.OwnerID, v.ID), v)
}
func (r *repository) Save(ctx context.Context, v Task, expected core.Version) error {
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := save(ctx, r.store, bucket, persistence.Key(v.OwnerID, v.ID), v, expected, func(v Task) core.Version { return v.Version }); err != nil {
			return err
		}
		return notification.Sync(ctx, r.store, v.OwnerID, notification.Input{Kind: "task.failed", Code: v.WaitingReason, Source: core.Object{Kind: "task", ID: v.ID}, AssistantID: v.AssistantID, Label: v.PlanTitle, Severity: "danger"}, v.State == Failed)
	})
}
func revisionKey(owner, id core.ID, number uint64) string {
	return persistence.Key(owner, id) + "/" + strconv.FormatUint(number, 10)
}
func (r *repository) AppendRevision(ctx context.Context, owner, id core.ID, v Revision) error {
	return r.store.Write(ctx, func(ctx context.Context, _ persistence.Tx) error {
		if _, err := r.Get(ctx, owner, id); err != nil {
			return err
		}
		if v.Number == 0 {
			return invalid("revision")
		}
		return insert(ctx, r.store, revisionBucket, revisionKey(owner, id, v.Number), v)
	})
}
func (r *repository) Revisions(ctx context.Context, owner, id core.ID, p core.PageRequest) (core.Page[Revision], error) {
	var out core.Page[Revision]
	err := r.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if _, err := r.Get(ctx, owner, id); err != nil {
			return err
		}
		var err error
		out, err = persistence.Page[Revision](ctx, r.store, revisionBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("RevisionScope", persistence.Key(owner, id))}, OrderBy: "Number"}, p, id, nil)
		return err
	})
	return out, err
}

func (s *service) emit(ctx context.Context, p core.Principal, v Task, event string) error {
	if s.options.Emit == nil {
		return core.NewError(core.Unavailable, "task event writer not configured")
	}
	return s.options.Emit(ctx, p, v, event)
}
func (s *service) Create(ctx context.Context, p core.Principal, req CreateRequest) (Task, error) {
	return mutate(ctx, s, p, "create", "", req.Mutation, req, false, false, func(ctx context.Context) (Task, error) {
		if !validText(req.Goal, 65536) {
			return Task{}, invalid("goal")
		}

		if s.options.CheckAssistant == nil {
			return Task{}, core.NewError(core.Unavailable, "assistant checker not configured")
		}
		if err := s.options.CheckAssistant(ctx, p, req.AssistantID); err != nil {
			return Task{}, err
		}
		for _, id := range req.ResourceIDs {
			if err := s.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: id}, Action: policy.Execute, ResourceID: id}); err != nil {
				return Task{}, err
			}
		}
		v := Task{ID: s.id(), OwnerID: p.OwnerID, AssistantID: req.AssistantID, Goal: req.Goal, State: Pending, Revision: 1, Spent: core.Money{Currency: "USD"}, Version: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
		if s.options.BindPlan != nil {
			if err := s.options.BindPlan(ctx, p, &v, req); err != nil {
				return Task{}, err
			}
		}
		if err := s.repo.Insert(ctx, v); err != nil {
			return v, err
		}
		if err := s.repo.AppendRevision(ctx, p.OwnerID, v.ID, Revision{Number: 1, Instructions: v.Goal, AcceptanceCriteria: req.AcceptanceCriteria, CreatedAt: s.now()}); err != nil {
			return v, err
		}
		return v, s.emit(ctx, p, v, "created")
	})
}
func (s *service) Get(ctx context.Context, p core.Principal, id core.ID) (Task, error) {
	var result Task
	err := s.read(ctx, p, id, policy.Read, false, func(ctx context.Context) error {
		var err error
		result, err = s.repo.Get(ctx, p.OwnerID, id)
		return err
	})
	return result, err
}
func (s *service) List(ctx context.Context, p core.Principal, q Query) (core.Page[Task], error) {
	var result core.Page[Task]
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
func (s *service) Revisions(ctx context.Context, p core.Principal, id core.ID, q core.PageRequest) (core.Page[Revision], error) {
	var result core.Page[Revision]
	err := s.read(ctx, p, id, policy.Read, false, func(ctx context.Context) error {
		var err error
		result, err = s.repo.Revisions(ctx, p.OwnerID, id, q)
		return err
	})
	return result, err
}
func (s *service) Revise(ctx context.Context, p core.Principal, req ReviseRequest) (Task, error) {
	return mutate(ctx, s, p, "revise", req.ID, req.Mutation, req, true, true, func(ctx context.Context) (Task, error) {
		v, err := s.repo.Get(ctx, p.OwnerID, req.ID)
		if err != nil {
			return v, err
		}
		if v.Version != req.Mutation.ExpectedVersion {
			return v, conflict()
		}
		if !validText(req.Instructions, 65536) {
			return v, invalid("instructions")
		}
		if v.State == Running {
			return v, core.NewError(core.FailedPrecondition, "pause active work before revising")
		}
		v.Revision++
		v.Version++
		v.State = Pending
		v.WaitingReason = ""
		v.ArtifactIDs = nil
		v.UpdatedAt = s.now()
		if err = s.repo.Save(ctx, v, req.Mutation.ExpectedVersion); err != nil {
			return v, err
		}
		if err = s.repo.AppendRevision(ctx, p.OwnerID, v.ID, Revision{Number: v.Revision, Instructions: req.Instructions, AcceptanceCriteria: req.AcceptanceCriteria, CreatedAt: s.now()}); err != nil {
			return v, err
		}
		return v, s.emit(ctx, p, v, "revised")
	})
}
func (s *service) control(ctx context.Context, p core.Principal, req ControlRequest, state State) (Task, error) {
	return mutate(ctx, s, p, string(state), req.ID, req.Mutation, req, true, true, func(ctx context.Context) (Task, error) {
		v, err := s.repo.Get(ctx, p.OwnerID, req.ID)
		if err != nil {
			return v, err
		}
		if v.Version != req.Mutation.ExpectedVersion {
			return v, conflict()
		}
		if v.State == Canceled || v.State == Completed || v.State == Failed || (state == Pending && v.State != Paused) {
			return v, core.NewError(core.FailedPrecondition, "invalid task transition")
		}
		v.State = state
		v.Version++
		v.UpdatedAt = s.now()
		if err = s.repo.Save(ctx, v, req.Mutation.ExpectedVersion); err != nil {
			return v, err
		}
		return v, s.emit(ctx, p, v, string(state))
	})
}
func (s *service) Pause(ctx context.Context, p core.Principal, r ControlRequest) (Task, error) {
	return s.control(ctx, p, r, Paused)
}
func (s *service) Resume(ctx context.Context, p core.Principal, r ControlRequest) (Task, error) {
	return s.control(ctx, p, r, Pending)
}
func (s *service) Cancel(ctx context.Context, p core.Principal, r ControlRequest) (Task, error) {
	return s.control(ctx, p, r, Canceled)
}
func (s *service) executionTask(ctx context.Context, e core.Execution, id core.ID) (Task, error) {
	if err := e.Validate(); err != nil {
		return Task{}, err
	}
	if e.TaskID != id {
		return Task{}, forbidden()
	}
	if s.options.ValidateExecution == nil {
		return Task{}, core.NewError(core.Unavailable, "execution verifier not configured")
	}
	if err := s.options.ValidateExecution(ctx, e); err != nil {
		return Task{}, err
	}
	v, err := s.repo.Get(ctx, e.Principal.OwnerID, id)
	if err != nil {
		return v, err
	}
	if v.AssistantID != e.AssistantID {
		return v, forbidden()
	}
	if v.State == Canceled || v.State == Completed || v.State == Failed || v.State == Paused {
		return v, core.NewError(core.FailedPrecondition, "task does not accept execution")
	}
	return v, nil
}
func (s *service) Report(ctx context.Context, e core.Execution, p Progress) (Task, error) {
	var result Task
	err := s.store.Write(ctx, func(ctx context.Context, _ persistence.Tx) error {
		if err := s.authorize(ctx, e.Principal, core.Object{Kind: objectKind, ID: p.TaskID}, policy.Write, false); err != nil {
			return err
		}
		v, err := s.executionTask(ctx, e, p.TaskID)
		if err != nil {
			return err
		}
		if p.State != Running && p.State != Waiting && p.State != Failed {
			return invalid("state")
		}
		v.State = p.State
		v.WaitingReason = p.Reason
		v.Version++
		v.UpdatedAt = s.now()
		if err = s.repo.Save(ctx, v, p.ExpectedVersion); err != nil {
			return err
		}
		result = v
		return nil
	})
	return result, err
}
func (s *service) Deliver(ctx context.Context, e core.Execution, req Delivery) (Task, error) {
	return mutate(ctx, s, e.Principal, "deliver", req.TaskID, req.Mutation, req, true, false, func(ctx context.Context) (Task, error) {
		v, err := s.executionTask(ctx, e, req.TaskID)
		if err != nil {
			return v, err
		}
		if v.Version != req.Mutation.ExpectedVersion {
			return v, conflict()
		}
		if req.Revision != v.Revision {
			return v, conflict()
		}
		if len(req.ArtifactIDs) == 0 || len(req.Checks) == 0 {
			return v, core.NewError(core.FailedPrecondition, "verification evidence required")
		}
		checks := map[string]bool{}
		for _, c := range req.Checks {
			if !c.Passed || len(c.EvidenceIDs) == 0 || !validText(c.Criterion, 4096) {
				return v, core.NewError(core.FailedPrecondition, "verification did not pass")
			}
			checks[c.Criterion] = true
		}
		revision, err := persistence.Load[Revision](ctx, s.store, revisionBucket, revisionKey(v.OwnerID, v.ID, v.Revision))
		if err != nil {
			return v, err
		}
		for _, criterion := range revision.AcceptanceCriteria {
			if !checks[criterion] {
				return v, core.NewError(core.FailedPrecondition, "acceptance criterion unverified")
			}
		}
		if s.options.VerifyEvidence == nil {
			return v, core.NewError(core.Unavailable, "evidence verifier not configured")
		}
		if err = s.options.VerifyEvidence(ctx, e, req); err != nil {
			return v, err
		}
		v.State = Completed
		v.ArtifactIDs = req.ArtifactIDs
		v.Version++
		v.UpdatedAt = s.now()
		if err = s.repo.Save(ctx, v, req.Mutation.ExpectedVersion); err != nil {
			return v, err
		}
		revision.ArtifactIDs = req.ArtifactIDs
		err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, revisionBucket, revisionKey(v.OwnerID, v.ID, v.Revision), revision)
		})
		return v, err
	})
}
