package schedule

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"molr.ai/runtime/internal/business/planstate"
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
	v, err := persistence.Load[Schedule](ctx, r.store, bucket, persistence.Key(owner, id))
	if err == nil {
		err = planstate.Visible(v.State)
	}
	return v, err
}
func (r *repository) List(ctx context.Context, owner core.ID, q Query) (core.Page[Schedule], error) {
	filters := []persistence.Filter{persistence.Eq("OwnerID", owner), {Field: "State", Op: "!=", Value: planstate.Deleted}}
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
	if v.State == "" {
		v.State = planstate.Active
	}
	return persistence.Insert(ctx, r.store, bucket, persistence.Key(v.OwnerID, v.ID), record(v))
}
func (r *repository) Save(ctx context.Context, v Schedule, expected core.Version) error {
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := persistence.SaveVersion(ctx, r.store, bucket, persistence.Key(v.OwnerID, v.ID), record(v), expected, func(v scheduleRecord) core.Version { return v.Version }); err != nil {
			return err
		}
		return planstate.RefreshHandoff(ctx, tx, v.OwnerID, core.Object{Kind: "schedule", ID: v.ID}, v.State)
	})
}
func (r *repository) Delete(ctx context.Context, owner, id core.ID, expected core.Version) error {
	return planstate.Delete(ctx, r.store, owner, core.Object{Kind: "schedule", ID: id}, expected)
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
		v := Schedule{ID: s.id(), OwnerID: p.OwnerID, Version: 1, Kind: req.Kind, AssistantID: req.AssistantID, Title: req.Title, Instructions: req.Instructions, Trigger: req.Trigger, EndAt: req.EndAt, State: planstate.Active, NextAt: nextAt}
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
		if req.State != planstate.Active && req.State != planstate.Paused {
			return v, invalid("state")
		}
		triggerChanged := v.Trigger != req.Trigger
		v.Kind = req.Kind
		v.AssistantID = req.AssistantID
		v.Title = req.Title
		v.Instructions = req.Instructions
		v.Trigger = req.Trigger
		v.EndAt = req.EndAt
		v.State = req.State
		// A content-only update preserves scheduling progress. Rewinding NextAt
		// here would replay occurrences whenever an assistant renamed a plan.
		if triggerChanged {
			nextAt, err := firstOccurrence(req.Trigger)
			if err != nil {
				return v, err
			}
			v.NextAt = nextAt
		}

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

func (r *repository) Due(ctx context.Context, now time.Time, limit int) ([]Schedule, error) {
	if limit < 1 || limit > 200 {
		return nil, invalid("limit")
	}
	return persistence.Select[Schedule](ctx, r.store, bucket, persistence.Query{
		Where: []persistence.Filter{persistence.Eq("State", planstate.Active),
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
		if schedule.State != planstate.Active {
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
