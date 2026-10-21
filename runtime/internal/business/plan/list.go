package plan

import (
	"context"
	"sort"
	"time"

	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

func (s *Coordinator) List(ctx context.Context, p core.Principal, from, to time.Time) ([]Plan, error) {
	return s.list(ctx, p, from, to, false)
}

func (s *Coordinator) list(ctx context.Context, p core.Principal, from, to time.Time, light bool) ([]Plan, error) {
	out := []Plan{}
	err := s.store.Read(ctx, func(ctx context.Context, _ persistence.Tx) error {
		if err := s.authorize(ctx, p, core.Object{}, policy.Read); err != nil {
			return err
		}
		if !from.IsZero() && (!to.After(from) || to.Sub(from) > 125*24*time.Hour) {
			return core.Invalid("range", "maximum calendar range is 125 days")
		}
		sourceQuery := persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID)}}
		if light {
			sourceQuery.Fields = []string{"ID", "OwnerID", "AssistantID", "Title", "DueAt", "StartAt", "EndAt", "Completed", "Version"}
		}
		todos, err := persistence.Select[todo.Todo](ctx, s.store, "business.todos", sourceQuery)
		if err != nil {
			return err
		}
		if light {
			sourceQuery.Fields = []string{"ID", "OwnerID", "AssistantID", "Title", "Kind", "Trigger", "EndAt", "Enabled", "NextAt", "Version"}
		}
		schedules, err := persistence.Select[schedule.Schedule](ctx, s.store, "business.schedules", sourceQuery)
		if err != nil {
			return err
		}
		sources := map[core.Object]*Plan{}
		visible := []*Plan{}
		add := func(row *Plan) {
			sources[row.Ref] = row
			if p.Kind != core.ActorAssistant || row.AssistantID == p.SubjectID {
				visible = append(visible, row)
			}
		}
		for i := range todos {
			v := &todos[i]
			if v.OwnerID == p.OwnerID {
				add(&Plan{Ref: core.Object{Kind: "todo", ID: v.ID}, Title: v.Title, AssistantID: v.AssistantID, Todo: v, Occurrences: []time.Time{}, BlockedBy: []core.Object{}})
			}
		}
		for i := range schedules {
			v := &schedules[i]
			if v.OwnerID == p.OwnerID {
				add(&Plan{Ref: core.Object{Kind: "schedule", ID: v.ID}, Title: v.Title, AssistantID: v.AssistantID, Schedule: v, Occurrences: []time.Time{}, BlockedBy: []core.Object{}})
			}
		}
		if err := s.loadListDetails(ctx, p.OwnerID, visible, light); err != nil {
			return err
		}
		// Readiness uses direct dependencies even when assigned to another assistant.
		// They supply status only and never enter the visible result list.
		loaded := map[core.Object]bool{}
		for _, row := range visible {
			loaded[row.Ref] = true
		}
		dependencies := []*Plan{}
		for _, row := range visible {
			for _, ref := range row.Links.Dependencies {
				if !valid(ref) {
					return core.Invalid("plan", "invalid plan")
				}
				if source := sources[ref]; source != nil && !loaded[ref] {
					loaded[ref] = true
					dependencies = append(dependencies, source)
				}
			}
		}
		if err := s.loadListDetails(ctx, p.OwnerID, dependencies, light); err != nil {
			return err
		}
		for _, row := range visible {
			if err := persistence.CheckContext(ctx); err != nil {
				return err
			}
			for _, ref := range row.Links.Dependencies {
				dependency := sources[ref]
				complete := false
				if dependency != nil {
					if len(dependency.Executions) > 0 {
						complete = dependency.Executions[0].State == "completed"
					} else if dependency.Todo != nil {
						complete = dependency.Todo.Completed
					}
				}
				if !complete {
					row.BlockedBy = append(row.BlockedBy, ref)
				}
			}
			row.Ready = len(row.BlockedBy) == 0 && !row.Links.Paused
			if row.Schedule != nil && !from.IsZero() {
				row.Occurrences, err = schedule.CalendarTimes(ctx, *row.Schedule, from, to)
				if err != nil {
					return err
				}
			}
			out = append(out, *row)
		}
		return nil
	})
	return out, err
}

// Keep SQL parameter counts bounded and avoid hydrating tasks for deleted or
// unrelated plans. All batches remain in List's original read transaction.
func (s *Coordinator) loadListDetails(ctx context.Context, owner core.ID, plans []*Plan, light bool) error {
	const batchSize = 256
	for start := 0; start < len(plans); start += batchSize {
		batch := plans[start:min(start+batchSize, len(plans))]
		byRef := make(map[core.Object]*Plan, len(batch))
		keys := make([]string, 0, len(batch))
		ids := map[string][]core.ID{}
		for _, row := range batch {
			if !valid(row.Ref) {
				return core.Invalid("plan", "invalid plan")
			}
			row.Links = Links{Ref: row.Ref, OwnerID: owner, Handoff: "notify", Dependencies: []core.Object{}, Runs: []Execution{}}
			row.Executions = []Execution{}
			byRef[row.Ref] = row
			keys = append(keys, key(owner, row.Ref))
			ids[row.Ref.Kind] = append(ids[row.Ref.Kind], row.Ref.ID)
		}
		links, err := persistence.Select[Links](ctx, s.store, bucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner), {Field: "key", Op: "in", Value: keys}}})
		if err != nil {
			return err
		}
		for _, link := range links {
			if row := byRef[link.Ref]; row != nil && link.OwnerID == owner {
				row.Links = link
				row.Executions = append(row.Executions, link.Runs...)
			}
		}
		query := persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner)}}
		for _, kind := range []string{"todo", "schedule"} {
			if len(ids[kind]) > 0 {
				query.Any = append(query.Any, []persistence.Filter{persistence.Eq("Plan.Kind", kind), {Field: "Plan.ID", Op: "in", Value: ids[kind]}})
			}
		}
		if light {
			query.Fields = []string{"ID", "OwnerID", "Plan", "State", "OccurrenceAt", "CreatedAt", "UpdatedAt"}
		}
		tasks, err := persistence.Select[task.Task](ctx, s.store, "business.tasks", query)
		if err != nil {
			return err
		}
		for _, run := range tasks {
			if row := byRef[run.Plan]; row != nil && run.OwnerID == owner {
				row.Executions = append(row.Executions, taskRun(run))
			}
		}
		for _, row := range batch {
			sort.Slice(row.Executions, func(i, j int) bool { return row.Executions[i].StartedAt.After(row.Executions[j].StartedAt) })
		}
	}
	return nil
}
