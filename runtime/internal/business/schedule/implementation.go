package schedule

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/trigger"
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

const objectKind = "schedule"
const bucket = "business.schedules"
const receiptBucket = "business.schedules.receipts"

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
func (r *repository) Get(ctx context.Context, owner, id core.ID) (Schedule, error) {
	return persistence.Load[Schedule](ctx, r.store, bucket, persistence.Key(owner, id))
}
func (r *repository) List(ctx context.Context, owner core.ID, q Query) (core.Page[Schedule], error) {
	filters := []persistence.Filter{persistence.Eq("OwnerID", owner)}
	if q.AssistantID != "" {
		filters = append(filters, persistence.Eq("AssistantID", q.AssistantID))
	}
	if q.Kind != "" {
		filters = append(filters, persistence.Eq("Kind", q.Kind))
	}

	request := q.Page
	q.Page = core.PageRequest{}
	return persistence.Page[Schedule](ctx, r.store, bucket, persistence.Query{Where: filters}, request, q, func(v Schedule) bool { return true })
}

// Keep the sortable timestamp private; public timestamps retain their original precision and zone.
type scheduleRecord struct {
	Schedule
	NextAtKey string
}

func record(v Schedule) scheduleRecord {
	out := scheduleRecord{Schedule: v}
	if v.NextAt != nil {
		out.NextAtKey = persistence.TimeKey(*v.NextAt)
	}
	return out
}
func (r *repository) Insert(ctx context.Context, v Schedule) error {
	return persistence.Insert(ctx, r.store, bucket, persistence.Key(v.OwnerID, v.ID), record(v))
}
func (r *repository) Save(ctx context.Context, v Schedule, expected core.Version) error {
	return persistence.SaveVersion(ctx, r.store, bucket, persistence.Key(v.OwnerID, v.ID), record(v), expected, func(v scheduleRecord) core.Version { return v.Version })
}
func (r *repository) Delete(ctx context.Context, owner, id core.ID, expected core.Version) error {
	return persistence.DeleteVersion[Schedule](ctx, r.store, bucket, persistence.Key(owner, id), expected, func(v Schedule) core.Version { return v.Version })
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
func (s *service) Create(ctx context.Context, p core.Principal, req CreateRequest) (Schedule, error) {
	return mutate(ctx, s, p, "create", "", req.Mutation, req, false, false, func(ctx context.Context) (Schedule, error) {
		if !validText(req.Title, 4096) || (req.Kind != Personal && req.Kind != AssistantSchedule) || (req.Kind == AssistantSchedule && req.AssistantID.Validate() != nil) || (req.Kind == Personal && req.AssistantID != "") {
			return Schedule{}, invalid("request")
		}
		if err := validateEnd(req.Kind, req.Trigger.StartAt, req.EndAt); err != nil {
			return Schedule{}, err
		}
		if err := req.Trigger.Validate(); err != nil {
			return Schedule{}, err
		}
		if err := s.checkAssistant(ctx, p, req.AssistantID); err != nil {
			return Schedule{}, err
		}
		nextAt, err := firstOccurrence(req.Trigger)
		if err != nil {
			return Schedule{}, err
		}
		v := Schedule{ID: s.id(), OwnerID: p.OwnerID, Version: 1, Kind: req.Kind, AssistantID: req.AssistantID, Title: req.Title, Instructions: req.Instructions, Trigger: req.Trigger, EndAt: req.EndAt, Enabled: nextAt != nil, NextAt: nextAt}
		return v, s.repo.Insert(ctx, v)
	})
}
func (s *service) Get(ctx context.Context, p core.Principal, id core.ID) (Schedule, error) {
	var v Schedule
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
func (s *service) List(ctx context.Context, p core.Principal, q Query) (core.Page[Schedule], error) {
	var result core.Page[Schedule]
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
func (s *service) Update(ctx context.Context, p core.Principal, req UpdateRequest) (Schedule, error) {
	return mutate(ctx, s, p, "update", req.ID, req.Mutation, req, true, false, func(ctx context.Context) (Schedule, error) {
		v, err := s.repo.Get(ctx, p.OwnerID, req.ID)
		if err != nil {
			return v, err
		}
		if v.Version != req.Mutation.ExpectedVersion {
			return v, conflict()
		}
		if !validText(req.Title, 4096) || (req.Kind != Personal && req.Kind != AssistantSchedule) || (req.Kind == AssistantSchedule && req.AssistantID.Validate() != nil) || (req.Kind == Personal && req.AssistantID != "") {
			return v, invalid("request")
		}
		if err := validateEnd(req.Kind, req.Trigger.StartAt, req.EndAt); err != nil {
			return Schedule{}, err
		}
		if err := req.Trigger.Validate(); err != nil {
			return v, err
		}
		if err := s.checkAssistant(ctx, p, req.AssistantID); err != nil {
			return v, err
		}
		triggerChanged := v.Trigger != req.Trigger || v.Enabled != req.Enabled
		v.Kind = req.Kind
		v.AssistantID = req.AssistantID
		v.Title = req.Title
		v.Instructions = req.Instructions
		v.Trigger = req.Trigger
		v.EndAt = req.EndAt
		v.Enabled = req.Enabled
		// A content-only update preserves scheduling progress. Rewinding NextAt
		// here would replay occurrences whenever an assistant renamed a plan.
		if triggerChanged {
			nextAt, err := firstOccurrence(req.Trigger)
			if err != nil {
				return v, err
			}
			if v.Enabled {
				v.Enabled = nextAt != nil
				v.NextAt = nextAt
			} else {
				v.NextAt = nil
			}
		}

		v.Version++
		return v, s.repo.Save(ctx, v, req.Mutation.ExpectedVersion)
	})
}
func (s *service) Delete(ctx context.Context, p core.Principal, req DeleteRequest) error {
	_, err := mutate(ctx, s, p, "delete", req.ID, req.Mutation, req, true, false, func(ctx context.Context) (struct{}, error) {
		v, err := s.repo.Get(ctx, p.OwnerID, req.ID)
		if err != nil {
			return struct{}{}, err
		}
		_ = v
		if err := checkPlanReferences(ctx, s.store, p.OwnerID, core.Object{Kind: "schedule", ID: req.ID}); err != nil {
			return struct{}{}, err
		}
		if err := s.repo.Delete(ctx, p.OwnerID, req.ID, req.Mutation.ExpectedVersion); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			err := tx.Delete(ctx, "business.plan-links", persistence.Key(p.OwnerID, core.ID("schedule-"+string(req.ID))))
			if persistence.Missing(err) {
				return nil
			}
			return err
		})
	})
	return err
}

func (r *repository) Due(ctx context.Context, now time.Time, limit int) ([]Schedule, error) {
	if limit < 1 || limit > 200 {
		return nil, invalid("limit")
	}
	return persistence.Select[Schedule](ctx, r.store, bucket, persistence.Query{
		Where: []persistence.Filter{persistence.Eq("Enabled", true),
			{Field: "NextAtKey", Op: ">", Value: ""},
			{Field: "NextAtKey", Op: "<=", Value: persistence.TimeKey(now)}},
		OrderBy: "NextAtKey", Limit: limit,
	})
}
func (r *repository) RecordOccurrence(ctx context.Context, owner core.ID, v Occurrence) (bool, error) {
	inserted := false
	err := r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		schedule, err := r.Get(ctx, owner, v.ScheduleID)
		if err != nil {
			return err
		}
		if schedule.Version != v.ScheduleVersion {
			return conflict()
		}
		if !schedule.Enabled {
			return core.NewError(core.FailedPrecondition, "schedule disabled")
		}
		key := persistence.Key(owner, v.ScheduleID) + "/" + strconv.FormatUint(uint64(v.ScheduleVersion), 10) + "/" + v.DueAt.UTC().Format(time.RFC3339Nano)
		var old Occurrence
		err = tx.Get(ctx, "business.occurrences", key, &old)
		if err == nil {
			return nil
		}
		if !persistence.Missing(err) {
			return err
		}
		// Existing installations keep their old occurrence ledger readable.
		// New schedule and dream occurrences share a namespaced ledger.
		inserted, err = trigger.RecordOccurrence(ctx, r.store, trigger.Occurrence{OwnerID: owner, Kind: "schedule", ID: v.ScheduleID, Revision: v.ScheduleVersion, At: v.DueAt})
		return err
	})
	return inserted, err
}

func validateEnd(kind Kind, start time.Time, end *time.Time) error {
	if end != nil && (kind != Personal || !end.After(start)) {
		return invalid("end_at")
	}
	return nil
}

func checkPlanReferences(ctx context.Context, store persistence.Store, owner core.ID, ref core.Object) error {
	tasks, err := persistence.Select[struct {
		OwnerID core.ID
		Plan    core.Object
	}](ctx, store, "business.tasks", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner), persistence.Eq("Plan.Kind", ref.Kind), persistence.Eq("Plan.ID", ref.ID)}, Limit: 1, Unordered: true})
	if err != nil {
		return err
	}
	for _, v := range tasks {
		if v.OwnerID == owner && v.Plan == ref {
			return core.NewError(core.FailedPrecondition, "pause a plan with execution history")
		}
	}
	links, err := persistence.Select[struct {
		OwnerID      core.ID
		Ref          core.Object
		Dependencies []core.Object
		Runs         []json.RawMessage
	}](ctx, store, "business.plan-links", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner)}, Any: [][]persistence.Filter{{persistence.Eq("Ref.Kind", ref.Kind), persistence.Eq("Ref.ID", ref.ID)}, {{Field: "Dependencies", Op: "has", Value: ref.Kind + "/" + string(ref.ID)}}}})
	if err != nil {
		return err
	}
	for _, v := range links {
		if v.OwnerID != owner {
			continue
		}
		if v.Ref == ref && len(v.Runs) > 0 {
			return core.NewError(core.FailedPrecondition, "pause a plan with execution history")
		}
		for _, dep := range v.Dependencies {
			if dep == ref {
				return core.NewError(core.FailedPrecondition, "plan has dependent arrangements")
			}
		}
	}
	return nil
}
