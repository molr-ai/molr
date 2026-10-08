package operation

import (
	"context"
	"encoding/json"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

const bucket = "agent.operations"

type repository struct{ store persistence.Store }

func NewRepository(store persistence.Store) Repository { return &repository{store} }
func (r *repository) Get(ctx context.Context, owner, id core.ID) (Operation, error) {
	return persistence.Load[Operation](ctx, r.store, bucket, persistence.Key(owner, id))
}
func (r *repository) FindByKey(ctx context.Context, owner core.ID, key string) (Operation, error) {
	items, err := persistence.Select[Operation](ctx, r.store, bucket, persistence.Query{
		Where:     []persistence.Filter{persistence.Eq("OwnerID", owner), persistence.Eq("IdempotencyKey", key)},
		Unordered: true, Limit: 1,
	})
	if err != nil {
		return Operation{}, err
	}
	if len(items) > 0 {
		return items[0], nil
	}
	return Operation{}, core.NewError(core.NotFound, "operation not found")
}
func (r *repository) List(ctx context.Context, owner core.ID, q Query) (core.Page[Operation], error) {
	filters := []persistence.Filter{persistence.Eq("OwnerID", owner)}
	if q.AssistantID != "" {
		filters = append(filters, persistence.Eq("AssistantID", q.AssistantID))
	}
	if q.TaskID != "" {
		filters = append(filters, persistence.Eq("TaskID", q.TaskID))
	}
	if len(q.States) > 0 {
		filters = append(filters, persistence.Filter{Field: "State", Op: "in", Value: q.States})
	}
	request := q.Page
	q.Page = core.PageRequest{}
	return persistence.Page[Operation](ctx, r.store, bucket, persistence.Query{Where: filters}, request, q, nil)
}

func valid(op Operation) error {
	for _, id := range []core.ID{op.ID, op.OwnerID, op.AssistantID, op.ExecutionID} {
		if err := id.Validate(); err != nil {
			return err
		}
	}
	if op.InputDigest == "" || op.IdempotencyKey == "" || op.Action == "" || op.Version == 0 {
		return core.Invalid("operation", "missing operation fields")
	}
	switch op.State {
	case Prepared, Running, Succeeded, Failed, Unknown, Canceled:
	default:
		return core.Invalid("state", "invalid operation state")
	}
	return nil
}
func (r *repository) Insert(ctx context.Context, op Operation) error {
	if err := valid(op); err != nil {
		return err
	}
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if _, err := r.Get(ctx, op.OwnerID, op.ID); err == nil {
			return core.NewError(core.Conflict, "operation exists")
		} else if !persistence.Missing(err) {
			return err
		}
		if _, err := r.FindByKey(ctx, op.OwnerID, op.IdempotencyKey); err == nil {
			return core.NewError(core.Conflict, "operation key exists")
		} else if !persistence.Missing(err) {
			return err
		}
		return tx.Put(ctx, bucket, persistence.Key(op.OwnerID, op.ID), op)
	})
}
func (r *repository) Save(ctx context.Context, op Operation, version core.Version) error {
	if err := valid(op); err != nil {
		return err
	}
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		old, err := r.Get(ctx, op.OwnerID, op.ID)
		if err != nil {
			return err
		}
		if version == 0 || old.Version != version || op.Version != version+1 {
			return core.NewError(core.Conflict, "operation version changed")
		}
		if old.State == Succeeded || old.State == Failed || old.State == Canceled {
			return core.NewError(core.FailedPrecondition, "terminal operation is immutable")
		}
		if old.TaskID != op.TaskID || old.AssistantID != op.AssistantID || old.ExecutionID != op.ExecutionID || old.Action != op.Action || old.ResourceID != op.ResourceID || old.InputDigest != op.InputDigest || old.IdempotencyKey != op.IdempotencyKey {
			return core.NewError(core.Conflict, "operation identity is immutable")
		}
		return tx.Put(ctx, bucket, persistence.Key(op.OwnerID, op.ID), op)
	})
}

type Options struct {
	Authorizer policy.Authorizer
	Now        func() time.Time
	Reconcile  func(context.Context, Operation) (Completion, error)
	Cancel     func(context.Context, Operation) error
}
type service struct {
	store   persistence.Store
	repo    Repository
	options Options
}

func NewService(store persistence.Store, options Options) Service {
	if options.Now == nil {
		options.Now = time.Now
	}
	return &service{store, NewRepository(store), options}
}
func (s *service) authorize(ctx context.Context, p core.Principal, id core.ID, action policy.Action) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if s.options.Authorizer == nil {
		return core.NewError(core.Unauthenticated, "authorization unavailable")
	}
	return s.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "operation", ID: id}, Action: action})
}
func (s *service) Get(ctx context.Context, p core.Principal, id core.ID) (Operation, error) {
	if err := s.authorize(ctx, p, id, policy.Read); err != nil {
		return Operation{}, err
	}
	op, err := s.repo.Get(ctx, p.OwnerID, id)
	if err == nil && p.Kind == core.ActorAssistant && op.AssistantID != p.SubjectID {
		return Operation{}, core.NewError(core.Forbidden, "foreign assistant operation")
	}
	return op, err
}
func (s *service) List(ctx context.Context, p core.Principal, q Query) (core.Page[Operation], error) {
	if err := s.authorize(ctx, p, p.OwnerID, policy.Read); err != nil {
		return core.Page[Operation]{}, err
	}
	if p.Kind == core.ActorAssistant {
		if q.AssistantID != "" && q.AssistantID != p.SubjectID {
			return core.Page[Operation]{}, core.NewError(core.Forbidden, "foreign assistant operations")
		}
		q.AssistantID = p.SubjectID
	}
	return s.repo.List(ctx, p.OwnerID, q)
}
func (s *service) Begin(ctx context.Context, req BeginRequest) (op Operation, err error) {
	if err = req.Execution.Validate(); err != nil {
		return
	}
	if err = req.Mutation.Validate(false); err != nil {
		return
	}
	if req.InputDigest == "" || req.Action == "" {
		return op, core.Invalid("operation", "action and digest required")
	}
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if s.options.Authorizer == nil {
			return core.NewError(core.Unauthenticated, "authorization unavailable")
		}
		if err := s.options.Authorizer.Require(ctx, req.Execution.Principal, policy.CheckRequest{Target: core.Object{Kind: "assistant", ID: req.Execution.AssistantID}, Action: policy.Execute}); err != nil {
			return err
		}
		existing, e := s.repo.FindByKey(ctx, req.Execution.Principal.OwnerID, req.Mutation.IdempotencyKey)
		if e == nil {
			if existing.InputDigest != req.InputDigest || existing.Action != req.Action || existing.ResourceID != req.ResourceID || existing.AssistantID != req.Execution.AssistantID || existing.TaskID != req.Execution.TaskID || existing.ExecutionID != req.Execution.Principal.ExecutionID {
				return core.NewError(core.Conflict, "operation key payload changed")
			}
			op = existing
			return nil
		}
		if !persistence.Missing(e) {
			return e
		}
		now := s.options.Now().UTC()
		op = Operation{ID: req.Execution.OperationID, OwnerID: req.Execution.Principal.OwnerID, AssistantID: req.Execution.AssistantID, TaskID: req.Execution.TaskID, ExecutionID: req.Execution.Principal.ExecutionID, ResourceID: req.ResourceID, Action: req.Action, State: Prepared, IdempotencyKey: req.Mutation.IdempotencyKey, InputDigest: req.InputDigest, Version: 1, CreatedAt: now, UpdatedAt: now}
		return s.repo.Insert(ctx, op)
	})
	return
}
func (s *service) bound(ctx context.Context, execution core.Execution, id core.ID) (Operation, error) {
	if err := execution.Validate(); err != nil {
		return Operation{}, err
	}
	op, err := s.Get(ctx, execution.Principal, id)
	if err != nil {
		return op, err
	}
	if op.ExecutionID != execution.Principal.ExecutionID || op.AssistantID != execution.AssistantID || op.ID != execution.OperationID {
		return Operation{}, core.NewError(core.Forbidden, "operation execution mismatch")
	}
	return op, nil
}
func (s *service) complete(ctx context.Context, op Operation, completion Completion) (Operation, error) {
	if completion.ExpectedVersion != op.Version {
		return Operation{}, core.NewError(core.Conflict, "stale completion")
	}
	if op.State == Succeeded || op.State == Failed || op.State == Canceled {
		return Operation{}, core.NewError(core.FailedPrecondition, "operation is terminal")
	}
	if completion.State != Succeeded && completion.State != Failed {
		return Operation{}, core.Invalid("completion", "terminal result required")
	}
	if len(completion.SafeResult) > 64<<20 || (len(completion.SafeResult) > 0 && !json.Valid(completion.SafeResult)) {
		return Operation{}, core.Invalid("result", "invalid or oversized result")
	}
	old := op.Version
	op.State = completion.State
	op.Result = completion.SafeResult
	op.ReceiptID = completion.ReceiptID
	op.FailureCode = completion.FailureCode
	op.Version++
	op.UpdatedAt = s.options.Now().UTC()
	return op, s.repo.Save(ctx, op, old)
}
func (s *service) Complete(ctx context.Context, e core.Execution, c Completion) (op Operation, err error) {
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		op, err = s.bound(ctx, e, c.ID)
		if err != nil {
			return err
		}
		op, err = s.complete(ctx, op, c)
		return err
	})
	return
}
func (s *service) MarkUnknown(ctx context.Context, e core.Execution, id core.ID) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		op, err := s.bound(ctx, e, id)
		if err != nil {
			return err
		}
		if op.State == Unknown {
			return nil
		}
		if op.State == Succeeded || op.State == Failed || op.State == Canceled {
			return core.NewError(core.FailedPrecondition, "known terminal result cannot become unknown")
		}
		version := op.Version
		op.State = Unknown
		op.Version++
		op.UpdatedAt = s.options.Now().UTC()
		return s.repo.Save(ctx, op, version)
	})
}
func (s *service) Cancel(ctx context.Context, p core.Principal, id core.ID) (Operation, error) {
	op, err := s.Get(ctx, p, id)
	if err != nil {
		return Operation{}, err
	}
	if err = s.authorize(ctx, p, id, policy.Write); err != nil {
		return Operation{}, err
	}
	if op.State == Canceled {
		return op, nil
	}
	if op.State == Succeeded || op.State == Failed {
		return Operation{}, core.NewError(core.FailedPrecondition, "operation already completed")
	}
	if op.State != Prepared {
		if s.options.Cancel == nil {
			return Operation{}, core.NewError(core.FailedPrecondition, "external cancellation requires confirmation")
		}
		if err = s.options.Cancel(ctx, op); err != nil {
			return Operation{}, err
		}
	}
	version := op.Version
	op.State = Canceled
	op.Version++
	op.UpdatedAt = s.options.Now().UTC()
	return op, s.repo.Save(ctx, op, version)
}
func (s *service) Reconcile(ctx context.Context, p core.Principal, req ReconcileRequest) (op Operation, err error) {
	op, err = s.Get(ctx, p, req.ID)
	if err != nil {
		return
	}
	if err = s.authorize(ctx, p, req.ID, policy.Write); err != nil {
		return
	}
	if req.ExpectedVersion != op.Version {
		return Operation{}, core.NewError(core.Conflict, "stale reconciliation")
	}
	if op.State == Succeeded || op.State == Failed {
		return op, nil
	}
	if s.options.Reconcile == nil {
		return Operation{}, core.NewError(core.FailedPrecondition, "provider evidence required")
	}
	completion, err := s.options.Reconcile(ctx, op)
	if err != nil {
		return Operation{}, err
	}
	completion.ID = op.ID
	completion.ExpectedVersion = op.Version
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		current, e := s.Get(ctx, p, op.ID)
		if e != nil {
			return e
		}
		op, e = s.complete(ctx, current, completion)
		return e
	})
	return
}
