package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/plan"
	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/business/watch"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type planTools struct {
	watches   *watch.Service
	store     persistence.Store
	plans     *plan.Coordinator
	todos     todo.Service
	schedules schedule.Service
}
type planView struct {
	Watch                 *watch.Watch    `json:",omitempty"`
	CurrentExecution      *plan.Execution `json:",omitempty"`
	ExecutionCount        int
	ExecutionsTruncated   bool
	InstructionsTruncated bool
	PlanID                string
	Mode                  string
	Title, Instructions   string
	Version               string
	StartAt, EndAt, DueAt *time.Time
	Timezone, Recurrence  string
	MissedPolicy          schedule.MissedPolicy
	MaxCatchUp            int
	Enabled               bool
	Dependencies          []string
	Handoff               string
	Paused, Ready         bool
	BlockedBy             []string
	Executions            []plan.Execution
	Occurrences           []time.Time
}

func planID(ref core.Object) string { return ref.Kind + ":" + string(ref.ID) }
func viewPlan(p plan.Plan) planView {
	v := planView{PlanID: planID(p.Ref), Title: p.Title, Version: planVersion(p), Dependencies: []string{}, BlockedBy: []string{}, Handoff: p.Links.Handoff, Paused: p.Links.Paused, Ready: p.Ready, Executions: p.Executions, Occurrences: p.Occurrences}
	for _, ref := range p.Links.Dependencies {
		v.Dependencies = append(v.Dependencies, planID(ref))
	}
	for _, ref := range p.BlockedBy {
		v.BlockedBy = append(v.BlockedBy, planID(ref))
	}
	if p.Watch != nil {
		v.Mode = "assistant_watch"
		v.Watch = p.Watch
		v.Instructions = p.Watch.Instructions
		v.StartAt = p.Watch.StartAt
		v.EndAt = p.Watch.EndAt
		v.Paused = p.Watch.Paused
		v.Enabled = !p.Watch.Paused && p.Watch.State != "completed"
	} else if p.Todo != nil {
		v.Mode = "todo"
		v.Instructions = p.Todo.Notes
		v.StartAt = p.Todo.StartAt
		v.EndAt = p.Todo.EndAt
		v.DueAt = p.Todo.DueAt
		v.Enabled = !p.Links.Paused
	} else {
		v.Mode = "assistant_schedule"
		v.Instructions = p.Schedule.Instructions
		v.StartAt = &p.Schedule.Trigger.StartAt
		v.EndAt = p.Schedule.EndAt
		v.Timezone = p.Schedule.Trigger.Timezone
		v.Recurrence = p.Schedule.Trigger.Recurrence
		v.MissedPolicy = p.Schedule.Trigger.MissedPolicy
		v.MaxCatchUp = p.Schedule.Trigger.MaxCatchUp
		v.Enabled = p.Schedule.Enabled
	}
	v.ExecutionCount = len(v.Executions)
	v.ExecutionsTruncated = len(v.Executions) > 20
	if v.ExecutionsTruncated {
		v.Executions = v.Executions[:20]
	}
	v.Executions = append([]plan.Execution{}, v.Executions...)
	for i := range v.Executions {
		v.Executions[i].Task = nil
		v.Executions[i].Instructions = ""
	}
	return v
}
func (t planTools) execute(ctx context.Context, e core.Execution, name string, raw json.RawMessage) (json.RawMessage, error) {
	if name == "plan_list" || name == "plan_read" {
		return t.run(ctx, e, name, raw)
	}
	var result json.RawMessage
	err := t.store.Write(ctx, func(ctx context.Context, _ persistence.Tx) error {
		var err error
		result, err = t.run(ctx, e, name, raw)
		return err
	})
	return result, err
}
func (t planTools) run(ctx context.Context, e core.Execution, name string, raw json.RawMessage) (json.RawMessage, error) {
	if err := (access{t.store}).execution(ctx, e); err != nil {
		return nil, err
	}
	r, err := tools.ParsePlanRequest(name, raw)
	if err != nil {
		return nil, err
	}
	if name == "plan_list" {
		var from, to time.Time
		if r.From != nil {
			from = *r.From
			to = *r.To
		}
		rows, err := t.plans.List(ctx, e.Principal, from, to)
		if err != nil {
			return nil, err
		}
		sort.Slice(rows, func(i, j int) bool { return planID(rows[i].Ref) < planID(rows[j].Ref) })
		scope := string(e.Principal.OwnerID) + "/" + string(e.AssistantID) + "/" + from.Format(time.RFC3339Nano) + "/" + to.Format(time.RFC3339Nano) + "/"
		after := ""
		if r.Cursor != "" {
			b, err := base64.RawURLEncoding.DecodeString(r.Cursor)
			if err != nil || !strings.HasPrefix(string(b), scope) {
				return nil, core.Invalid("cursor", "invalid plan cursor")
			}
			after = strings.TrimPrefix(string(b), scope)
		}
		limit := r.Limit
		if limit == 0 {
			limit = 20
		}
		page := core.Page[planView]{Items: []planView{}}
		for _, row := range rows {
			if planID(row.Ref) <= after {
				continue
			}
			if len(page.Items) == limit {
				page.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(scope + page.Items[len(page.Items)-1].PlanID))
				break
			}
			v := viewPlan(row)
			v.Executions = nil
			if v.Watch != nil {
				w := *v.Watch
				w.Watcher.Script = ""
				w.Checkpoint = nil
				w.Pending = nil
				w.Recent = nil
				w.Batch = nil
				w.Instructions = ""
				w.AuthorizationFingerprint = ""
				v.Watch = &w
			}
			v.ExecutionsTruncated = v.ExecutionCount > 0
			if len([]rune(v.Instructions)) > 240 {
				v.Instructions = string([]rune(v.Instructions)[:240])
				v.InstructionsTruncated = true
			}
			page.Items = append(page.Items, v)
		}
		return json.Marshal(page)
	}
	var currentExecution *plan.Execution
	ref := core.Object{}
	if r.PlanID != "" {
		ref, err = tools.PlanReference(r.PlanID)
		if err != nil {
			return nil, err
		}
	}
	if r.ExecutionID != "" {
		if r.ExecutionID != e.TaskID {
			return nil, denied()
		}
		task, err := task.NewRepository(t.store).Get(ctx, e.Principal.OwnerID, r.ExecutionID)
		if err != nil {
			return nil, err
		}
		if task.AssistantID != e.AssistantID {
			return nil, denied()
		}
		currentExecution = &plan.Execution{ID: task.ID, State: string(task.State), Title: task.PlanTitle, Instructions: task.PlanInstructions, PlanVersion: task.PlanVersion, OccurrenceAt: task.OccurrenceAt, StartedAt: task.CreatedAt}
		ref = task.Plan
	}
	if name == "plan_manage" {
		ref, err = t.manage(ctx, e, r, ref)
		if err != nil {
			return nil, err
		}
		if r.Action == "delete" {
			return json.Marshal(map[string]any{"PlanID": r.PlanID, "Deleted": true})
		}
	}

	value, err := t.plans.Read(ctx, e.Principal, ref)
	if err != nil {
		return nil, err
	}
	out := viewPlan(value)
	out.CurrentExecution = currentExecution
	return json.Marshal(out)
}

func planVersion(p plan.Plan) string {
	var version core.Version
	if p.Watch != nil {
		version = p.Watch.Version
	} else if p.Todo != nil {
		version = p.Todo.Version
	} else {
		version = p.Schedule.Version
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", planID(p.Ref), version, p.Links.Version)))
	return hex.EncodeToString(sum[:])
}
func hasPlanField(r tools.PlanRequest, names ...string) bool {
	for _, name := range names {
		if _, ok := r.Fields[name]; ok {
			return true
		}
	}
	return false
}
func (t planTools) manage(ctx context.Context, e core.Execution, r tools.PlanRequest, ref core.Object) (core.Object, error) {
	var current plan.Plan
	var err error
	if r.Action == "create" {
		ref, err = t.create(ctx, e, r)
		if err != nil {
			return ref, err
		}
		current, err = t.plans.Read(ctx, e.Principal, ref)
		if err != nil {
			return ref, err
		}
	} else {
		current, err = t.plans.Read(ctx, e.Principal, ref)
		if err != nil {
			return ref, err
		}
		if planVersion(current) != r.ExpectedVersion {
			return ref, core.NewError(core.Conflict, "plan changed; read it again")
		}
	}
	mutation := core.Mutation{IdempotencyKey: string(e.OperationID)}
	if current.Watch != nil {
		return t.manageWatch(ctx, e, r, current)
	}
	if current.Todo != nil {
		mutation.ExpectedVersion = current.Todo.Version
	} else {
		mutation.ExpectedVersion = current.Schedule.Version
	}
	switch r.Action {
	case "create": // Content was created above. Relations below belong to the same transaction.
	case "update":
		if hasPlanField(r, "title", "instructions", "start_at", "end_at", "due_at", "timezone", "recurrence", "missed_policy", "max_catch_up") {
			if current.Todo != nil {
				if hasPlanField(r, "timezone", "recurrence", "missed_policy", "max_catch_up") {
					return ref, core.Invalid("mode", "scheduled fields do not apply to a once plan")
				}
				v := *current.Todo
				if hasPlanField(r, "title") {
					v.Title = r.Title
				}
				if hasPlanField(r, "instructions") {
					v.Notes = r.Instructions
				}
				if hasPlanField(r, "start_at") {
					v.StartAt = r.StartAt
				}
				if hasPlanField(r, "end_at") {
					v.EndAt = r.EndAt
				}
				if hasPlanField(r, "due_at") {
					v.DueAt = r.DueAt
				}
				_, err = t.todos.Update(ctx, e.Principal, todo.UpdateRequest{ID: ref.ID, Completed: v.Completed, CreateRequest: todo.CreateRequest{Mutation: mutation, AssistantID: e.AssistantID, Title: v.Title, Notes: v.Notes, StartAt: v.StartAt, EndAt: v.EndAt, DueAt: v.DueAt}})
			} else {
				if hasPlanField(r, "due_at") {
					return ref, core.Invalid("due_at", "scheduled plans do not accept due_at")
				}
				v := *current.Schedule
				if hasPlanField(r, "title") {
					v.Title = r.Title
				}
				if hasPlanField(r, "instructions") {
					v.Instructions = r.Instructions
				}
				if hasPlanField(r, "start_at") {
					if r.StartAt == nil {
						return ref, core.Invalid("start_at", "scheduled plans require a start time")
					}
					v.Trigger.StartAt = *r.StartAt
				}
				if hasPlanField(r, "end_at") {
					v.EndAt = r.EndAt
				}
				if hasPlanField(r, "timezone") {
					v.Trigger.Timezone = r.Timezone
				}
				if hasPlanField(r, "recurrence") {
					v.Trigger.Recurrence = r.Recurrence
				}
				if hasPlanField(r, "missed_policy") {
					v.Trigger.MissedPolicy = schedule.MissedPolicy(r.MissedPolicy)
					if v.Trigger.MissedPolicy != schedule.CatchUp {
						v.Trigger.MaxCatchUp = 0
					}
				}
				if hasPlanField(r, "max_catch_up") {
					v.Trigger.MaxCatchUp = r.MaxCatchUp
				}
				if hasPlanField(r, "start_at", "timezone", "recurrence") {
					v.Enabled = true
				}
				_, err = t.schedules.Update(ctx, e.Principal, schedule.UpdateRequest{ID: ref.ID, Enabled: v.Enabled, CreateRequest: schedule.CreateRequest{Mutation: mutation, Kind: v.Kind, AssistantID: e.AssistantID, Title: v.Title, Instructions: v.Instructions, Trigger: v.Trigger, EndAt: v.EndAt}})
			}
			if err != nil {
				return ref, err
			}
		}
	case "delete":
		if ref.Kind == "todo" {
			err = t.todos.Delete(ctx, e.Principal, todo.DeleteRequest{ID: ref.ID, Mutation: mutation})
		} else {
			err = t.schedules.Delete(ctx, e.Principal, schedule.DeleteRequest{ID: ref.ID, Mutation: mutation})
		}
		return ref, err
	case "execute":
		if current.Schedule != nil && current.Schedule.Trigger.Recurrence != "" && r.OccurrenceAt == nil {
			return ref, core.Invalid("occurrence_at", "select a scheduled occurrence")
		}
		if r.OccurrenceAt != nil {
			if current.Schedule == nil {
				return ref, core.Invalid("occurrence_at", "only scheduled plans have occurrences")
			}
			times, err := schedule.CalendarTimes(ctx, *current.Schedule, *r.OccurrenceAt, r.OccurrenceAt.Add(time.Second))
			if err != nil || len(times) != 1 || !times[0].Equal(*r.OccurrenceAt) {
				return ref, core.Invalid("occurrence_at", "not a planned occurrence")
			}
		}
		mutation.ExpectedVersion = current.Links.Version
		_, err = t.plans.Execute(ctx, e.Principal, plan.ExecuteRequest{Ref: ref, Mutation: mutation, Action: "start", OccurrenceAt: r.OccurrenceAt})
		return ref, err
	}
	if hasPlanField(r, "dependencies", "handoff", "paused") {
		deps := current.Links.Dependencies
		handoff := current.Links.Handoff
		paused := current.Links.Paused
		if hasPlanField(r, "dependencies") {
			deps = []core.Object{}
			for _, id := range r.Dependencies {
				dep, _ := tools.PlanReference(id)
				deps = append(deps, dep)
			}
		}
		if hasPlanField(r, "handoff") {
			handoff = r.Handoff
		}
		if hasPlanField(r, "paused") {
			paused = r.Paused
		}
		mutation.ExpectedVersion = current.Links.Version
		_, err = t.plans.Configure(ctx, e.Principal, plan.ConfigureRequest{Ref: ref, Mutation: mutation, Dependencies: deps, Handoff: handoff, Paused: paused})
		if err != nil {
			return ref, err
		}
	}
	return ref, nil
}
func (t planTools) create(ctx context.Context, e core.Execution, r tools.PlanRequest) (core.Object, error) {
	mutation := core.Mutation{IdempotencyKey: string(e.OperationID)}
	if r.Mode == "assistant_watch" {
		if t.watches == nil {
			return core.Object{}, core.NewError(core.Unavailable, "watch service unavailable")
		}
		if r.Watcher == nil || hasPlanField(r, "due_at", "timezone", "recurrence", "missed_policy", "max_catch_up", "dependencies", "handoff") {
			return core.Object{}, core.Invalid("watcher", "watcher required; scheduling fields are not accepted")
		}
		w, err := t.watches.Save(ctx, e.Principal, watch.Request{Mutation: mutation, AssistantID: e.AssistantID, Title: r.Title, Instructions: r.Instructions, Watcher: *r.Watcher, StartAt: r.StartAt, EndAt: r.EndAt, Paused: r.Paused})
		return core.Object{Kind: "watch", ID: w.ID}, err
	}
	if r.Watcher != nil {
		return core.Object{}, core.Invalid("watcher", "only continuous plans accept watcher")
	}
	if r.Mode == "todo" {
		if hasPlanField(r, "timezone", "recurrence", "missed_policy", "max_catch_up") {
			return core.Object{}, core.Invalid("mode", "scheduled fields do not apply to a once plan")
		}
		v, err := t.todos.Create(ctx, e.Principal, todo.CreateRequest{Mutation: mutation, AssistantID: e.AssistantID, Title: r.Title, Notes: r.Instructions, StartAt: r.StartAt, EndAt: r.EndAt, DueAt: r.DueAt})
		return core.Object{Kind: "todo", ID: v.ID}, err
	}
	if r.StartAt == nil || hasPlanField(r, "due_at") {
		return core.Object{}, core.Invalid("start_at", "scheduled plans require start_at and do not accept due_at")
	}
	missed := schedule.MissedPolicy(r.MissedPolicy)
	if missed == "" {
		missed = schedule.Skip
	}
	v, err := t.schedules.Create(ctx, e.Principal, schedule.CreateRequest{Mutation: mutation, Kind: schedule.AssistantSchedule, AssistantID: e.AssistantID, Title: r.Title, Instructions: r.Instructions, Trigger: schedule.Trigger{StartAt: *r.StartAt, Timezone: r.Timezone, Recurrence: r.Recurrence, MissedPolicy: missed, MaxCatchUp: r.MaxCatchUp}, EndAt: r.EndAt})
	return core.Object{Kind: "schedule", ID: v.ID}, err
}

func (t planTools) manageWatch(ctx context.Context, e core.Execution, r tools.PlanRequest, p plan.Plan) (core.Object, error) {
	if t.watches == nil {
		return p.Ref, core.NewError(core.Unavailable, "watch service unavailable")
	}
	w := *p.Watch
	m := core.Mutation{IdempotencyKey: string(e.OperationID), ExpectedVersion: w.Version}
	if r.Action == "create" {
		return p.Ref, nil
	}
	if r.Action == "delete" {
		return p.Ref, t.watches.Delete(ctx, e.Principal, w.ID, m)
	}
	if hasPlanField(r, "due_at", "timezone", "recurrence", "missed_policy", "max_catch_up", "dependencies", "handoff", "occurrence_at") {
		return p.Ref, core.Invalid("watch", "watch plans do not have occurrences or dependency handoffs")
	}
	if hasPlanField(r, "title") {
		w.Title = r.Title
	}
	if hasPlanField(r, "instructions") {
		w.Instructions = r.Instructions
	}
	if hasPlanField(r, "watcher") {
		if r.Watcher == nil {
			return p.Ref, core.Invalid("watcher", "required")
		}
		w.Watcher = *r.Watcher
	}
	if hasPlanField(r, "start_at") {
		w.StartAt = r.StartAt
	}
	if hasPlanField(r, "end_at") {
		w.EndAt = r.EndAt
	}
	if hasPlanField(r, "paused") {
		w.Paused = r.Paused
	}
	if r.Action == "execute" {
		w.Paused = false
	}
	_, err := t.watches.Save(ctx, e.Principal, watch.Request{Mutation: m, ID: w.ID, AssistantID: w.AssistantID, Title: w.Title, Instructions: w.Instructions, Watcher: w.Watcher, StartAt: w.StartAt, EndAt: w.EndAt, Paused: w.Paused})
	return p.Ref, err
}
