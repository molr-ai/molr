// Package debugtrace records optional, owner-only execution diagnostics.
// Recording never authorizes or replays a business operation.
package debugtrace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"strconv"
	"sync/atomic"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type Options struct {
	ResetContext             func(context.Context, core.Principal, core.ID, core.Mutation, uint64) error
	Enabled                  bool
	MaxRuns, MaxPayloadBytes int
	Administrator            func(context.Context, core.Principal) error
	Authorize                func(context.Context, core.Principal) error
	Now                      func() time.Time
}
type Step struct {
	Summary     *ToolSummary `json:",omitempty"`
	LogicalID   core.ID      `json:",omitempty"`
	Attempt     int          `json:",omitempty"`
	MaxAttempts int          `json:",omitempty"`
	NextRetryAt *time.Time   `json:",omitempty"`
	ErrorReason string       `json:",omitempty"`
	HTTPStatus  int          `json:",omitempty"`
	Sequence    int
	Revision    uint64
	Kind, State string
	OperationID core.ID
	At          time.Time
	DataStatus  string
	Preview     string
	Name        string
}
type Run struct {
	ID, OwnerID, AssistantID, EventID, ExecutionID core.ID
	Trigger                                        string
	Attempt                                        int
	State, Reason                                  string
	StartedAt                                      time.Time
	FinishedAt                                     *time.Time
	Incomplete                                     bool
	Steps                                          []Step
	Bytes                                          int
	FirstRevision, LastRevision                    uint64
	Source                                         *SourceContent `json:",omitempty"`
}
type Query struct {
	AssistantID core.ID
	Page        core.PageRequest
}
type Payload struct {
	Status string
	Data   json.RawMessage
}
type Reader interface {
	WatchRevision(context.Context, core.Principal) (persistence.Watch, error)
	Retention(context.Context, core.Principal) (RetentionStatus, error)
	Trajectory(context.Context, core.Principal, TrajectoryQuery) (TrajectoryPage, error)
	Runs(context.Context, core.Principal, Query) (core.Page[Run], error)
	Get(context.Context, core.Principal, core.ID) (Run, error)
	Detail(context.Context, core.Principal, core.ID, int) (Payload, error)
	Checkpoint(context.Context, core.Principal, core.ID) (Payload, error)
	Revision(context.Context, core.Principal) (uint64, error)
}
type ResetRequest struct {
	Mutation         core.Mutation
	ExpectedSequence *uint64
}
type Control interface {
	Reader
	AdminRetention(context.Context, core.Principal) (RetentionSettings, error)
	SetRetention(context.Context, core.Principal, RetentionRequest) (Retention, error)
	ResetContext(context.Context, core.Principal, core.ID, ResetRequest) error
}

type Service struct {
	store   persistence.Store
	options Options
}
type reference struct {
	owner, assistant, id core.ID
	incomplete           atomic.Bool
}
type contextKey struct{}

func New(store persistence.Store, options Options) *Service {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.MaxRuns == 0 {
		options.MaxRuns = 1000
	}
	if options.MaxPayloadBytes == 0 {
		options.MaxPayloadBytes = 4 << 20
	}
	return &Service{store, options}
}
func (s *Service) enabled() bool {
	return s != nil && s.options.Enabled
}
func (s *Service) authorize(ctx context.Context, p core.Principal) error {
	if p.RequireOwner() != nil || s.options.Authorize == nil {
		return core.NewError(core.Forbidden, "owner debug access required")
	}
	if err := s.options.Authorize(ctx, p); err != nil {
		return err
	}
	if !s.enabled() {
		return core.NewError(core.Forbidden, "workbench disabled")
	}
	return nil
}
func (s *Service) bump(ctx context.Context, tx persistence.Tx, owner core.ID) error {
	var revision uint64
	err := tx.Get(ctx, "debug.revisions", string(owner), &revision)
	if err != nil && !persistence.Missing(err) {
		return err
	}
	if err := tx.Put(ctx, "debug.revisions", string(owner), revision+1); err != nil {
		return err
	}
	return tx.NotifyAfterCommit(ctx, revisionSignal(owner))
}

// Begin binds a distinct attempt to this context. It is called only by the trusted Loop.
func (s *Service) Begin(ctx context.Context, owner, assistant, event, execution core.ID, trigger string, attempt int, data any) context.Context {
	if !s.enabled() {
		return ctx
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return ctx
	}
	if attempt < 1 {
		attempt = 1
	}
	run := Run{ID: core.ID(hex.EncodeToString(id[:])), OwnerID: owner, AssistantID: assistant, EventID: event, ExecutionID: execution, Trigger: trigger, Attempt: attempt, State: "running", StartedAt: s.options.Now().UTC(), Steps: []Step{}}
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := saveRun(ctx, tx, run); err != nil {
			return err
		}
		if err := s.prune(ctx, tx, owner, run.ID); err != nil {
			return err
		}
		return s.bump(ctx, tx, owner)
	})
	if err != nil {
		log.Printf("debug trace start unavailable: %s", core.CodeOf(err))
		return ctx
	}
	ctx = context.WithValue(ctx, contextKey{}, &reference{owner: owner, assistant: assistant, id: run.ID})
	assistantRow, _ := persistence.Load[json.RawMessage](ctx, s.store, "business.assistants", persistence.Key(owner, assistant))
	grants, _ := persistence.Select[json.RawMessage](ctx, s.store, "security.grants", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner), persistence.Eq("AssistantID", assistant)}})
	permissions := []json.RawMessage{}
	for _, raw := range grants {
		var g struct{ OwnerID, AssistantID core.ID }
		if json.Unmarshal(raw, &g) == nil && g.OwnerID == owner && g.AssistantID == assistant {
			permissions = append(permissions, raw)
		}
	}
	source := SourceContent{Status: "unavailable", Origin: "capture"}
	eventData, _ := json.Marshal(data)
	_ = s.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		source = s.sourceContent(ctx, tx, owner, assistant, eventData, "capture")
		return nil
	})
	s.Record(ctx, "run.start", "started", "", map[string]any{"Event": data, "Source": source, "Assistant": assistantRow, "Permissions": permissions})
	return ctx
}

// RecordCheckpoint records a successful save inside the caller's transaction.
// Owner controls and legacy migrations have no active loop trace, so they get
// a separate, bounded recording. Diagnostic failures never fail the save.
func (s *Service) RecordCheckpoint(ctx context.Context, owner, assistant, event, execution core.ID, data any) {
	if !s.enabled() {
		return
	}
	ref, _ := ctx.Value(contextKey{}).(*reference)
	if ref != nil && ref.owner == owner && ref.assistant == assistant {
		s.Record(ctx, "checkpoint.saved", "succeeded", "", data)
		return
	}
	ctx = context.WithValue(ctx, contextKey{}, (*reference)(nil))
	ctx = s.Begin(ctx, owner, assistant, event, execution, "checkpoint", 1, nil)
	s.Record(ctx, "checkpoint.saved", "succeeded", "", data)
	s.Finish(ctx, "succeeded", "")
}

func payloadKey(owner, id core.ID, sequence int) string {
	return persistence.Key(owner, id) + "/" + strconv.Itoa(sequence)
}

// Record accepts data before credentials are added to provider requests. It sanitizes
// diagnostic fields and keeps failures or payload limits explicit in step metadata.
func (s *Service) Record(ctx context.Context, kind, state string, operation core.ID, data any) {
	ref, _ := ctx.Value(contextKey{}).(*reference)
	if s == nil || ref == nil {
		return
	}
	if !s.enabled() {
		ref.incomplete.Store(true)
		return
	}
	encoded, status := sanitize(data, s.options.MaxPayloadBytes)
	if status == "too_large" || status == "unavailable" {
		ref.incomplete.Store(true)
	}
	capture, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	err := s.store.Write(capture, func(ctx context.Context, tx persistence.Tx) error {
		run, err := persistence.Load[Run](ctx, tx.Store(), "debug.runs", persistence.Key(ref.owner, ref.id))
		if err != nil {
			return err
		}
		if err := s.prune(ctx, tx, ref.owner, ref.id); err != nil {
			return err
		}
		sequence := len(run.Steps) + 1
		step := Step{Sequence: sequence, Kind: kind, State: state, OperationID: operation, At: s.options.Now().UTC(), DataStatus: status}
		var revision uint64
		if err := tx.Get(ctx, "debug.revisions", string(ref.owner), &revision); err != nil && !persistence.Missing(err) {
			return err
		}
		step.Revision = revision + 1
		step.Name, step.Preview = trajectoryPreview(kind, encoded)
		step.Summary = toolSummary(kind, state, encoded)
		if len(kind) >= 6 && kind[:6] == "model." {
			var metadata struct {
				LogicalID                    core.ID
				Attempt, MaxAttempts, Status int
				NextAt                       *time.Time
				Reason                       string
				Failure                      *struct{ Status int }
			}
			if json.Unmarshal(encoded, &metadata) == nil {
				step.LogicalID, step.Attempt, step.MaxAttempts, step.NextRetryAt, step.ErrorReason, step.HTTPStatus = metadata.LogicalID, metadata.Attempt, metadata.MaxAttempts, metadata.NextAt, metadata.Reason, metadata.Status
				if metadata.Failure != nil {
					step.HTTPStatus = metadata.Failure.Status
				}
			}
		}
		if kind == "run.start" {
			var value struct{ Source *SourceContent }
			if json.Unmarshal(encoded, &value) == nil && value.Source != nil {
				run.Source = sourceSummary(*value.Source)
				if value.Source.Status == "redacted" {
					status = "redacted"
					step.DataStatus = status
				}
			} else {
				run.Source = &SourceContent{Status: status}
			}
		}
		run.Steps = append(run.Steps, step)
		run.Bytes += len(encoded)
		run.Incomplete = run.Incomplete || ref.incomplete.Load()
		if err := savePayload(ctx, tx, run, sequence, Payload{Status: status, Data: encoded}); err != nil {
			return err
		}
		if err := saveRun(ctx, tx, run); err != nil {
			return err
		}
		return s.bump(ctx, tx, ref.owner)
	})
	if err != nil {
		ref.incomplete.Store(true)
		log.Printf("debug trace capture unavailable: %s", core.CodeOf(err))
	}
}
func (s *Service) Finish(ctx context.Context, state, reason string) {
	ref, _ := ctx.Value(contextKey{}).(*reference)
	if s == nil || ref == nil {
		return
	}
	s.Record(ctx, "run.end", state, "", map[string]string{"Reason": reason})
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	_ = s.store.Write(finish, func(ctx context.Context, tx persistence.Tx) error {
		run, err := persistence.Load[Run](ctx, tx.Store(), "debug.runs", persistence.Key(ref.owner, ref.id))
		if err != nil {
			return err
		}
		now := s.options.Now().UTC()
		run.FinishedAt = &now
		run.State = state
		run.Reason = reason
		run.Incomplete = run.Incomplete || ref.incomplete.Load()
		if err := saveRun(ctx, tx, run); err != nil {
			return err
		}
		if err := s.prune(ctx, tx, ref.owner, ""); err != nil {
			return err
		}
		return s.bump(ctx, tx, ref.owner)
	})
}
func (s *Service) Runs(ctx context.Context, p core.Principal, q Query) (out core.Page[Run], err error) {
	if err = s.authorize(ctx, p); err != nil {
		return
	}
	page, err := q.Page.Normalize()
	if err != nil {
		return out, err
	}
	err = s.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		query := visibleQuery(p.OwnerID)
		if q.AssistantID != "" {
			query.Where = append(query.Where, persistence.Eq("AssistantID", q.AssistantID))
		}
		query.OrderBy = "StartedAtKey"
		query.Descending = true
		query.ThenBy = "ID"
		query.ThenDescending = true
		query.Limit = page.Limit + 1
		if page.Cursor != "" {
			anchor, err := persistence.Load[Run](ctx, tx.Store(), "debug.summaries", persistence.Key(p.OwnerID, core.ID(page.Cursor)))
			if err != nil || q.AssistantID != "" && anchor.AssistantID != q.AssistantID {
				return core.Invalid("cursor", "expired run cursor")
			}
			query.After = []any{persistence.TimeKey(anchor.StartedAt), string(anchor.ID), persistence.Key(p.OwnerID, anchor.ID)}
		}
		rows, err := persistence.Select[Run](ctx, tx.Store(), "debug.summaries", query)
		if err != nil {
			return err
		}
		if len(rows) > page.Limit {
			rows = rows[:page.Limit]
			out.NextCursor = string(rows[len(rows)-1].ID)
		}
		out.Items = rows
		return nil
	})
	return
}
func (s *Service) Get(ctx context.Context, p core.Principal, id core.ID) (Run, error) {
	if err := s.authorize(ctx, p); err != nil {
		return Run{}, err
	}
	run, err := persistence.Load[Run](ctx, s.store, "debug.runs", persistence.Key(p.OwnerID, id))
	if err != nil {
		return Run{}, err
	}
	return run, nil
}
func (s *Service) Detail(ctx context.Context, p core.Principal, id core.ID, sequence int) (Payload, error) {
	run, err := s.Get(ctx, p, id)
	if err != nil {
		return Payload{}, err
	}
	if sequence < 1 || sequence > len(run.Steps) {
		return Payload{}, core.NewError(core.NotFound, "step unavailable")
	}
	var payload Payload
	err = s.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var err error
		payload, err = loadPayload(ctx, tx, run, sequence)
		if err != nil {
			return err
		}
		if sequence != 1 || (payload.Status != "captured" && payload.Status != "redacted") {
			return nil
		}
		var data map[string]json.RawMessage
		if json.Unmarshal(payload.Data, &data) != nil || data["Source"] != nil {
			return nil
		}
		source := s.sourceForRun(ctx, tx, run)
		data["Source"], _ = json.Marshal(source)
		encoded, status := sanitize(data, s.options.MaxPayloadBytes)
		if status == "captured" || status == "redacted" {
			payload.Data = encoded
			if status == "redacted" || source.Status == "redacted" {
				payload.Status = "redacted"
			}
		}
		return nil
	})
	return payload, err
}
func (s *Service) Checkpoint(ctx context.Context, p core.Principal, id core.ID) (Payload, error) {
	if err := s.authorize(ctx, p); err != nil {
		return Payload{}, err
	}
	if _, err := persistence.Load[json.RawMessage](ctx, s.store, "business.assistants", persistence.Key(p.OwnerID, id)); err != nil {
		return Payload{}, err
	}
	var raw json.RawMessage
	err := s.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var err error
		raw, err = loadCheckpoint(ctx, tx, p.OwnerID, id)
		return err
	})
	if persistence.Missing(err) {
		raw, err = json.Marshal(map[string]any{"AssistantID": id, "Sequence": 0, "Context": map[string]any{"Inputs": []any{}}})
	}
	if err != nil {
		return Payload{}, err
	}
	data, status := sanitize(raw, s.options.MaxPayloadBytes)
	return Payload{Status: status, Data: data}, nil
}
func revisionSignal(owner core.ID) persistence.SignalKey {
	return persistence.SignalKey{Namespace: "debug.revision", Key: string(owner)}
}

// Register before reading Revision so a concurrent commit cannot be missed.
// Signals only announce changes; every read still checks current authorization.
func (s *Service) WatchRevision(ctx context.Context, p core.Principal) (persistence.Watch, error) {
	if err := s.authorize(ctx, p); err != nil {
		return nil, err
	}
	return s.store.Watch(revisionSignal(p.OwnerID))
}

func (s *Service) Revision(ctx context.Context, p core.Principal) (uint64, error) {
	if err := s.authorize(ctx, p); err != nil {
		return 0, err
	}
	n, err := persistence.Load[uint64](ctx, s.store, "debug.revisions", string(p.OwnerID))
	if persistence.Missing(err) {
		return 0, nil
	}
	return n, err
}

// Recover marks recordings left open by a previous process; it never changes jobs.
func (s *Service) Recover(ctx context.Context) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		runs, err := persistence.Select[Run](ctx, tx.Store(), "debug.summaries", persistence.Query{Where: []persistence.Filter{persistence.Eq("State", "running")}})
		if err != nil {
			return err
		}
		for _, run := range runs {
			if run.State == "running" {
				var err error
				run, err = persistence.Load[Run](ctx, tx.Store(), "debug.runs", persistence.Key(run.OwnerID, run.ID))
				if err != nil {
					return err
				}
				now := s.options.Now().UTC()
				run.State = "interrupted"
				run.Incomplete = true
				run.FinishedAt = &now
				if err := saveRun(ctx, tx, run); err != nil {
					return err
				}
				if err := s.bump(ctx, tx, run.OwnerID); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func saveRun(ctx context.Context, tx persistence.Tx, run Run) error {
	if len(run.Steps) > 0 {
		run.FirstRevision = run.Steps[0].Revision
		run.LastRevision = run.Steps[len(run.Steps)-1].Revision
	}
	if err := tx.Put(ctx, "debug.runs", persistence.Key(run.OwnerID, run.ID), run); err != nil {
		return err
	}
	run.Steps = nil
	return tx.Put(ctx, "debug.summaries", persistence.Key(run.OwnerID, run.ID), run)
}

// ResetContext is gated independently of the normal assistant/chat API.
func (s *Service) ResetContext(ctx context.Context, p core.Principal, id core.ID, req ResetRequest) error {
	if err := s.authorize(ctx, p); err != nil {
		return err
	}
	if err := id.Validate(); err != nil {
		return err
	}
	if req.ExpectedSequence == nil {
		return core.Invalid("expected_sequence", "required")
	}
	if s.options.ResetContext == nil {
		return core.NewError(core.FailedPrecondition, "context reset unavailable")
	}
	if err := s.options.ResetContext(ctx, p, id, req.Mutation, *req.ExpectedSequence); err != nil {
		return err
	}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return s.bump(ctx, tx, p.OwnerID) })
}
