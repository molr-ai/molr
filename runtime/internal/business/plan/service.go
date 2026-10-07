package plan

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"sort"
	"strings"
	"time"
)

const bucket = "business.plan-links"

type Coordinator struct {
	store      persistence.Store
	authorizer policy.Authorizer
	tasks      task.Service
}

func New(store persistence.Store, authorizer policy.Authorizer, tasks task.Service) *Coordinator {
	return &Coordinator{store, authorizer, tasks}
}
func id() core.ID {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return core.ID(hex.EncodeToString(b[:]))
}
func key(owner core.ID, ref core.Object) string {
	return persistence.Key(owner, core.ID(ref.Kind+"-"+string(ref.ID)))
}
func valid(ref core.Object) bool {
	return (ref.Kind == "todo" || ref.Kind == "schedule") && ref.ID.Validate() == nil
}
func (s *Coordinator) authorize(ctx context.Context, p core.Principal, ref core.Object, action policy.Action) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if ref.ID == "" {
		ref.Kind = "todo"
	} else if !valid(ref) {
		return core.Invalid("plan", "invalid plan")
	}
	return s.authorizer.Require(ctx, p, policy.CheckRequest{Target: ref, Action: action})
}
func (s *Coordinator) links(ctx context.Context, owner core.ID, ref core.Object) (Links, error) {
	v, err := persistence.Load[Links](ctx, s.store, bucket, key(owner, ref))
	if persistence.Missing(err) {
		return Links{Ref: ref, OwnerID: owner, Handoff: "notify", Dependencies: []core.Object{}, Runs: []Execution{}}, nil
	}
	return v, err
}
func (s *Coordinator) source(ctx context.Context, owner core.ID, ref core.Object) (Plan, error) {
	p := Plan{Ref: ref, Executions: []Execution{}, Occurrences: []time.Time{}, BlockedBy: []core.Object{}}
	if !valid(ref) {
		return p, core.Invalid("plan", "invalid plan")
	}
	var err error
	if ref.Kind == "todo" {
		v, e := todo.NewRepository(s.store).Get(ctx, owner, ref.ID)
		err = e
		p.Todo = &v
		p.Title = v.Title
		p.AssistantID = v.AssistantID
	} else {
		v, e := schedule.NewRepository(s.store).Get(ctx, owner, ref.ID)
		err = e
		p.Schedule = &v
		p.Title = v.Title
		p.AssistantID = v.AssistantID
	}
	if err != nil {
		return p, err
	}
	p.Links, err = s.links(ctx, owner, ref)
	return p, err
}
func taskRun(t task.Task) Execution {
	r := Execution{ID: t.ID, State: string(t.State), Title: t.PlanTitle, Instructions: t.PlanInstructions, PlanVersion: t.PlanVersion, OccurrenceAt: t.OccurrenceAt, StartedAt: t.CreatedAt, Task: &t}
	if t.State == task.Completed || t.State == task.Canceled || t.State == task.Failed {
		r.CompletedAt = &t.UpdatedAt
	}
	return r
}
func (s *Coordinator) runs(ctx context.Context, owner core.ID, p Plan) ([]Execution, error) {
	out := append([]Execution{}, p.Links.Runs...)
	rows, err := persistence.Scan[task.Task](ctx, s.store, "business.tasks")
	if err != nil {
		return nil, err
	}
	for _, t := range rows {
		if t.OwnerID == owner && t.Plan == p.Ref {
			out = append(out, taskRun(t))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out, nil
}
func (s *Coordinator) ready(ctx context.Context, owner core.ID, p Plan) ([]core.Object, error) {
	blocked := []core.Object{}
	for _, ref := range p.Links.Dependencies {
		dependency, err := s.source(ctx, owner, ref)
		if persistence.Missing(err) {
			blocked = append(blocked, ref)
			continue
		}
		if err != nil {
			return nil, err
		}
		runs, err := s.runs(ctx, owner, dependency)
		if err != nil {
			return nil, err
		}
		complete := len(runs) > 0 && runs[0].State == "completed"
		if len(runs) == 0 && dependency.Todo != nil {
			complete = dependency.Todo.Completed
		}
		if !complete {
			blocked = append(blocked, ref)
		}
	}
	return blocked, nil
}
func (s *Coordinator) List(ctx context.Context, p core.Principal, from, to time.Time) ([]Plan, error) {
	out := []Plan{}
	err := s.store.Read(ctx, func(ctx context.Context, _ persistence.Tx) error {
		if err := s.authorize(ctx, p, core.Object{}, policy.Read); err != nil {
			return err
		}
		if !from.IsZero() && (!to.After(from) || to.Sub(from) > 125*24*time.Hour) {
			return core.Invalid("range", "maximum calendar range is 125 days")
		}
		todos, err := persistence.Scan[todo.Todo](ctx, s.store, "business.todos")
		if err != nil {
			return err
		}
		schedules, err := persistence.Scan[schedule.Schedule](ctx, s.store, "business.schedules")
		if err != nil {
			return err
		}
		refs := []core.Object{}
		for _, v := range todos {
			if v.OwnerID == p.OwnerID && (p.Kind != core.ActorAssistant || v.AssistantID == p.SubjectID) {
				refs = append(refs, core.Object{Kind: "todo", ID: v.ID})
			}
		}
		for _, v := range schedules {
			if v.OwnerID == p.OwnerID && (p.Kind != core.ActorAssistant || v.AssistantID == p.SubjectID) {
				refs = append(refs, core.Object{Kind: "schedule", ID: v.ID})
			}
		}
		for _, ref := range refs {
			row, err := s.source(ctx, p.OwnerID, ref)
			if err != nil {
				return err
			}
			row.Executions, err = s.runs(ctx, p.OwnerID, row)
			if err != nil {
				return err
			}
			row.BlockedBy, err = s.ready(ctx, p.OwnerID, row)
			if err != nil {
				return err
			}
			row.Ready = len(row.BlockedBy) == 0 && !row.Links.Paused
			if row.Schedule != nil && !from.IsZero() {
				row.Occurrences, err = schedule.CalendarTimes(ctx, *row.Schedule, from, to)
				if err != nil {
					return err
				}
			}
			out = append(out, row)
		}
		return nil
	})
	return out, err
}
func mutate[T any](ctx context.Context, s *Coordinator, p core.Principal, ref core.Object, m core.Mutation, action string, payload any, fn func(context.Context, persistence.Tx) (T, error)) (T, error) {
	var result T
	if err := m.Validate(false); err != nil {
		return result, err
	}
	data, _ := json.Marshal(payload)
	digest := sha256.Sum256(data)
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := s.authorize(ctx, p, ref, policy.Write); err != nil {
			return err
		}
		receiptKey := persistence.Key(p.OwnerID, core.ID(action+"-"+m.IdempotencyKey))
		var receipt struct {
			Digest [32]byte
			Result T
		}
		err := tx.Get(ctx, "business.plan-receipts", receiptKey, &receipt)
		if err == nil {
			if receipt.Digest != digest {
				return core.NewError(core.Conflict, "idempotency conflict")
			}
			result = receipt.Result
			return nil
		}
		if !persistence.Missing(err) {
			return err
		}
		result, err = fn(ctx, tx)
		if err != nil {
			return err
		}
		receipt.Digest = digest
		receipt.Result = result
		return tx.Put(ctx, "business.plan-receipts", receiptKey, receipt)
	})
	return result, err
}
func (s *Coordinator) Configure(ctx context.Context, p core.Principal, req ConfigureRequest) (Links, error) {
	return mutate(ctx, s, p, req.Ref, req.Mutation, "configure", req, func(ctx context.Context, tx persistence.Tx) (Links, error) {
		source, err := s.source(ctx, p.OwnerID, req.Ref)
		if err != nil {
			return Links{}, err
		}
		v := source.Links
		if req.Mutation.ExpectedVersion != v.Version {
			return v, core.NewError(core.Conflict, "plan links changed")
		}
		if len(req.Dependencies) > 32 || (req.Handoff != "notify" && req.Handoff != "automatic") || (req.Handoff == "automatic" && (source.AssistantID == "" || len(req.Dependencies) == 0)) {
			return v, core.Invalid("handoff", "invalid handoff")
		}
		seen := map[core.Object]bool{}
		var visit func(core.Object) error
		visit = func(ref core.Object) error {
			if ref == req.Ref {
				return core.Invalid("dependencies", "cycle")
			}
			if seen[ref] {
				return nil
			}
			seen[ref] = true
			if len(seen) > 1000 {
				return core.Invalid("dependencies", "too many dependencies")
			}
			if err := s.authorize(ctx, p, ref, policy.Read); err != nil {
				return err
			}
			next, err := s.source(ctx, p.OwnerID, ref)
			if err != nil {
				return err
			}
			for _, dep := range next.Links.Dependencies {
				if err := visit(dep); err != nil {
					return err
				}
			}
			return nil
		}
		for _, ref := range req.Dependencies {
			if err := visit(ref); err != nil {
				return v, err
			}
		}
		// Repeated prerequisites have no unique occurrence matching yet: disallow ambiguous chains.
		for _, ref := range req.Dependencies {
			dep, err := s.source(ctx, p.OwnerID, ref)
			if err != nil {
				return v, err
			}
			if dep.Schedule != nil && dep.Schedule.Trigger.Recurrence != "" {
				return v, core.Invalid("dependencies", "prerequisite must be a one-time plan")
			}
		}
		v.Dependencies = req.Dependencies
		v.Handoff = req.Handoff
		v.Paused = req.Paused
		v.Version++
		return v, tx.Put(ctx, bucket, key(p.OwnerID, req.Ref), v)
	})
}
func instructions(p Plan) string {
	if p.Todo != nil {
		if p.Todo.Notes != "" && p.Todo.Notes != p.Title {
			return p.Title + "\n" + p.Todo.Notes
		}
		return p.Title
	}
	if p.Schedule.Instructions != "" {
		return p.Schedule.Instructions
	}
	return p.Title
}
func version(p Plan) core.Version {
	if p.Todo != nil {
		return p.Todo.Version
	}
	return p.Schedule.Version
}
func (s *Coordinator) executionInstructions(ctx context.Context, owner core.ID, p Plan) (string, error) {
	text := instructions(p)
	for _, ref := range p.Links.Dependencies {
		dep, err := s.source(ctx, owner, ref)
		if err != nil {
			return "", err
		}
		runs, err := s.runs(ctx, owner, dep)
		if err != nil {
			return "", err
		}
		if len(runs) == 0 {
			continue
		}
		latest := runs[0]
		text += "\n\nCompleted prerequisite: " + dep.Title + "\n" + latest.Result
		if latest.Task != nil {
			data, _ := json.Marshal(latest.Task.ArtifactIDs)
			text += "\nOutput references (existing access rules apply): " + string(data)
		}
	}
	return text, nil
}
func (s *Coordinator) BindTask(ctx context.Context, p core.Principal, v *task.Task, req task.CreateRequest) error {
	ref := req.Plan
	if ref.ID == "" {
		ref = core.Object{Kind: "todo", ID: id()}
		item := todo.Todo{ID: ref.ID, OwnerID: p.OwnerID, AssistantID: v.AssistantID, Title: v.Goal, Notes: v.Goal, Version: 1}
		if err := todo.NewRepository(s.store).Insert(ctx, item); err != nil {
			return err
		}
	}
	source, err := s.source(ctx, p.OwnerID, ref)
	if err != nil {
		return err
	}
	if source.AssistantID != v.AssistantID {
		return core.NewError(core.Forbidden, "plan belongs to another assignee")
	}
	blocked, err := s.ready(ctx, p.OwnerID, source)
	if err != nil {
		return err
	}
	if len(blocked) > 0 || source.Links.Paused {
		return core.NewError(core.FailedPrecondition, "plan prerequisites are incomplete or plan paused")
	}
	runs, err := s.runs(ctx, p.OwnerID, source)
	if err != nil {
		return err
	}
	for _, run := range runs {
		if run.State == "running" || run.State == "pending" || run.State == "waiting" || run.State == "paused" {
			return core.NewError(core.FailedPrecondition, "an execution is still active")
		}
	}
	v.Plan = ref
	v.PlanTitle = source.Title
	v.PlanInstructions, err = s.executionInstructions(ctx, p.OwnerID, source)
	if err != nil {
		return err
	}
	v.Goal = v.PlanInstructions
	v.PlanVersion = version(source)
	v.OccurrenceAt = req.OccurrenceAt
	return nil
}
func (s *Coordinator) Execute(ctx context.Context, p core.Principal, req ExecuteRequest) (Execution, error) {
	return mutate(ctx, s, p, req.Ref, req.Mutation, "execute", req, func(ctx context.Context, tx persistence.Tx) (Execution, error) {
		source, err := s.source(ctx, p.OwnerID, req.Ref)
		if err != nil {
			return Execution{}, err
		}
		if source.Links.Version != req.Mutation.ExpectedVersion {
			return Execution{}, core.NewError(core.Conflict, "plan changed")
		}
		blocked, err := s.ready(ctx, p.OwnerID, source)
		if err != nil {
			return Execution{}, err
		}
		continuing := false
		if source.AssistantID == "" && req.Action == "complete" {
			for _, run := range source.Links.Runs {
				if run.State == "running" && sameOccurrence(run.OccurrenceAt, req.OccurrenceAt) {
					continuing = true
				}
			}
		}
		if !continuing && (len(blocked) > 0 || source.Links.Paused) {
			return Execution{}, core.NewError(core.FailedPrecondition, "plan is blocked")
		}
		if len(req.Result) > 8192 {
			return Execution{}, core.Invalid("result", "too long")
		}
		if req.Action != "start" && req.Action != "complete" {
			return Execution{}, core.Invalid("action", "start or complete required")
		}
		if source.AssistantID != "" {
			if req.Action != "start" {
				return Execution{}, core.Invalid("action", "assistant completion needs execution evidence")
			}
			t, err := s.tasks.Create(ctx, p, task.CreateRequest{Plan: req.Ref, OccurrenceAt: req.OccurrenceAt, Mutation: core.Mutation{IdempotencyKey: executionKey(req.Mutation.IdempotencyKey)}, AssistantID: source.AssistantID, Goal: instructions(source)})
			return taskRun(t), err
		}
		if source.Schedule != nil && source.Schedule.Trigger.Recurrence != "" {
			if req.OccurrenceAt == nil {
				return Execution{}, core.Invalid("occurrence", "select an occurrence")
			}
			times, err := schedule.CalendarTimes(ctx, *source.Schedule, *req.OccurrenceAt, req.OccurrenceAt.Add(time.Second))
			if err != nil || len(times) != 1 || !times[0].Equal(*req.OccurrenceAt) {
				return Execution{}, core.Invalid("occurrence", "not a planned occurrence")
			}
		}
		now := time.Now().UTC()
		v := source.Links
		index := -1
		for i, r := range v.Runs {
			same := r.OccurrenceAt == nil && req.OccurrenceAt == nil || r.OccurrenceAt != nil && req.OccurrenceAt != nil && r.OccurrenceAt.Equal(*req.OccurrenceAt)
			if same {
				index = i
				break
			}
		}
		if index < 0 {
			v.Runs = append(v.Runs, Execution{ID: id(), Title: source.Title, Instructions: instructions(source), PlanVersion: version(source), OccurrenceAt: req.OccurrenceAt, StartedAt: now, State: "running"})
			index = len(v.Runs) - 1
		}
		if v.Runs[index].State == "completed" {
			return v.Runs[index], nil
		}
		if req.Action == "complete" {
			v.Runs[index].State = "completed"
			v.Runs[index].CompletedAt = &now
			v.Runs[index].Result = strings.TrimSpace(req.Result)
		}
		v.Version++
		if err := tx.Put(ctx, bucket, key(p.OwnerID, req.Ref), v); err != nil {
			return Execution{}, err
		}
		if source.Todo != nil {
			item := *source.Todo
			item.Completed = v.Runs[index].State == "completed"
			item.Version++
			if err := todo.NewRepository(s.store).Save(ctx, item, source.Todo.Version); err != nil {
				return Execution{}, err
			}
		}
		return v.Runs[index], nil
	})
}

// Migrate wraps legacy standalone tasks once, preserving execution IDs and history.
func (s *Coordinator) Migrate(ctx context.Context) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		rows, err := persistence.Scan[task.Task](ctx, s.store, "business.tasks")
		if err != nil {
			return err
		}
		for _, v := range rows {
			if v.Plan.ID != "" {
				continue
			}
			ref := core.Object{Kind: "todo", ID: id()}
			item := todo.Todo{ID: ref.ID, OwnerID: v.OwnerID, AssistantID: v.AssistantID, Title: v.Goal, Notes: v.Goal, Completed: v.State == task.Completed, Version: 1}
			if err := todo.NewRepository(s.store).Insert(ctx, item); err != nil {
				return err
			}
			v.Plan = ref
			v.PlanTitle = v.Goal
			v.PlanInstructions = v.Goal
			v.PlanVersion = 1
			if err := tx.Put(ctx, "business.tasks", persistence.Key(v.OwnerID, v.ID), v); err != nil {
				return err
			}
		}
		return nil
	})
}

// startTrusted only runs from the transactional scheduler, after checking the stored plan and assignee.
func (s *Coordinator) startTrusted(ctx context.Context, owner core.ID, p Plan, at *time.Time, emit bool) (task.Task, error) {
	a, err := assistant.NewRepository(s.store).Get(ctx, owner, p.AssistantID)
	if err != nil {
		return task.Task{}, err
	}
	if a.State != assistant.Active {
		return task.Task{}, core.NewError(core.FailedPrecondition, "assistant inactive")
	}
	text, err := s.executionInstructions(ctx, owner, p)
	if err != nil {
		return task.Task{}, err
	}
	now := time.Now().UTC()
	v := task.Task{ID: id(), OwnerID: owner, AssistantID: p.AssistantID, Plan: p.Ref, PlanTitle: p.Title, PlanInstructions: text, PlanVersion: version(p), OccurrenceAt: at, Goal: text, State: task.Pending, Revision: 1, Version: 1, Spent: core.Money{Currency: "USD"}, CreatedAt: now, UpdatedAt: now}
	repo := task.NewRepository(s.store)
	if err := repo.Insert(ctx, v); err != nil {
		return v, err
	}
	if err := repo.AppendRevision(ctx, owner, v.ID, task.Revision{Number: 1, Instructions: v.Goal, CreatedAt: now}); err != nil {
		return v, err
	}
	if emit {
		err = event.NewWriter(s.store).AppendAssistant(ctx, event.AssistantEvent{ID: id(), OwnerID: owner, AssistantID: v.AssistantID, Kind: event.ResumeRequested, DeliveryMode: event.WhenIdle, TaskID: v.ID, CreatedAt: now})
	}
	return v, err
}
func (s *Coordinator) ScheduleReady(ctx context.Context, owner, id core.ID) (bool, error) {
	p, err := s.source(ctx, owner, core.Object{Kind: "schedule", ID: id})
	if err != nil {
		return false, err
	}
	blocked, err := s.ready(ctx, owner, p)
	return len(blocked) == 0 && !p.Links.Paused, err
}
func (s *Coordinator) StartScheduled(ctx context.Context, source schedule.Schedule, at time.Time) (core.ID, error) {
	p, err := s.source(ctx, source.OwnerID, core.Object{Kind: "schedule", ID: source.ID})
	if err != nil {
		return "", err
	}
	v, err := s.startTrusted(ctx, source.OwnerID, p, &at, false)
	return v.ID, err
}

// Tick performs explicitly enabled handoffs exactly once. Notify mode remains visible as Ready.
func (s *Coordinator) Tick(ctx context.Context, now time.Time) error {
	all, err := persistence.Scan[Links](ctx, s.store, bucket)
	if err != nil {
		return err
	}
	for _, entry := range all {
		if err := s.syncHandoff(ctx, entry); err != nil {
			return err
		}
		if entry.Handoff != "automatic" || entry.Paused || entry.Dispatched || len(entry.Dependencies) == 0 || entry.Ref.Kind != "todo" {
			continue
		}
		err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			p, err := s.source(ctx, entry.OwnerID, entry.Ref)
			if persistence.Missing(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if p.Links.Dispatched || p.Links.Paused || p.Links.Handoff != "automatic" || p.AssistantID == "" {
				return nil
			}
			if p.Todo.StartAt != nil && p.Todo.StartAt.After(now) {
				return nil
			}
			blocked, err := s.ready(ctx, entry.OwnerID, p)
			if err != nil || len(blocked) > 0 {
				return err
			}
			runs, err := s.runs(ctx, entry.OwnerID, p)
			if err != nil {
				return err
			}
			if len(runs) == 0 {
				if _, err = s.startTrusted(ctx, entry.OwnerID, p, nil, true); err != nil {
					return err
				}
			}
			p.Links.Dispatched = true
			p.Links.Version++
			return tx.Put(ctx, bucket, key(entry.OwnerID, entry.Ref), p.Links)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func executionKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "plan-" + hex.EncodeToString(sum[:])
}

func sameOccurrence(a, b *time.Time) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Equal(*b)
}

// Compute the condition within the same snapshot that writes its notification.
func (s *Coordinator) syncHandoff(ctx context.Context, entry Links) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		in := notification.Input{Kind: "plan.ready", Code: "ready", Source: entry.Ref, Severity: "neutral"}
		p, err := s.source(ctx, entry.OwnerID, entry.Ref)
		if persistence.Missing(err) {
			return notification.Sync(ctx, s.store, entry.OwnerID, in, false)
		}
		if err != nil {
			return err
		}
		in.Label = p.Title
		in.AssistantID = p.AssistantID
		blocked, err := s.ready(ctx, entry.OwnerID, p)
		if err != nil {
			return err
		}
		runs, err := s.runs(ctx, entry.OwnerID, p)
		if err != nil {
			return err
		}
		ready := p.Links.Handoff == "notify" && !p.Links.Paused && !p.Links.Dispatched && len(p.Links.Dependencies) > 0 && len(blocked) == 0 && len(runs) == 0
		return notification.Sync(ctx, s.store, entry.OwnerID, in, ready)
	})
}
