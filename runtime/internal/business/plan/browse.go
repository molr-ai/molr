package plan

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
	"time"

	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

// ExecutionSummary is enough to render state and navigate to an execution.
// Instructions, results and task bodies are loaded only on expansion.
type ExecutionSummary struct {
	ID           core.ID
	State        string
	OccurrenceAt *time.Time
	StartedAt    time.Time
	CompletedAt  *time.Time
}
type Summary struct {
	Ref            core.Object
	Title          string
	AssistantID    core.ID
	Todo           *todo.Todo
	Schedule       *schedule.Schedule
	Links          Links
	Executions     []ExecutionSummary // Latest plus occurrences in the requested window/page.
	ExecutionCount int
	HasSuccessors  bool
	Occurrences    []time.Time
	BlockedBy      []core.Object
	Ready          bool
}

func executionSummary(run Execution) ExecutionSummary {
	return ExecutionSummary{run.ID, run.State, run.OccurrenceAt, run.StartedAt, run.CompletedAt}
}
func summarize(p Plan, include func(Execution) bool) Summary {
	links := p.Links
	links.Runs = nil
	out := Summary{Ref: p.Ref, Title: p.Title, AssistantID: p.AssistantID, Todo: p.Todo, Schedule: p.Schedule, Links: links,
		Executions: []ExecutionSummary{}, ExecutionCount: len(p.Executions), Occurrences: p.Occurrences, BlockedBy: p.BlockedBy, Ready: p.Ready}
	seen := map[time.Time]bool{}
	for i, run := range p.Executions {
		if run.OccurrenceAt != nil {
			at := run.OccurrenceAt.UTC()
			if seen[at] {
				continue
			}
			seen[at] = true
		}
		if i == 0 || include != nil && include(run) {
			out.Executions = append(out.Executions, executionSummary(run))
		}
	}
	return out
}
func successorRefs(plans []Plan) map[core.Object]bool {
	out := map[core.Object]bool{}
	for _, p := range plans {
		for _, ref := range p.Links.Dependencies {
			out[ref] = true
		}
	}
	return out
}

// Overview shares status projection with Events and excludes task bodies from reads.
func (s *Coordinator) Overview(ctx context.Context, p core.Principal, from, to time.Time) ([]Summary, error) {
	plans, err := s.list(ctx, p, from, to, true)
	if err != nil {
		return nil, err
	}
	successors := successorRefs(plans)
	out := make([]Summary, 0, len(plans))
	for _, plan := range plans {
		// CalendarTimes may include occurrences starting before the window.
		dates := map[time.Time]bool{}
		for _, at := range plan.Occurrences {
			dates[at.UTC()] = true
		}
		item := summarize(plan, func(run Execution) bool { return run.OccurrenceAt != nil && dates[run.OccurrenceAt.UTC()] })
		item.HasSuccessors = successors[item.Ref]
		out = append(out, item)
	}
	return out, nil
}

// Detail loads one editable source and only the state needed by its actions.
func (s *Coordinator) Detail(ctx context.Context, p core.Principal, ref core.Object, at *time.Time) (Summary, error) {
	var out Summary
	err := s.store.Read(ctx, func(ctx context.Context, _ persistence.Tx) error {
		if err := s.authorize(ctx, p, ref, policy.Read); err != nil {
			return err
		}
		source, err := s.source(ctx, p.OwnerID, ref)
		if err != nil {
			return err
		}
		if p.Kind == core.ActorAssistant && source.AssistantID != p.SubjectID {
			return core.NewError(core.Forbidden, "plan belongs to another assistant")
		}
		if err := s.loadListDetails(ctx, p.OwnerID, []*Plan{&source}, true); err != nil {
			return err
		}
		source.BlockedBy, err = s.ready(ctx, p.OwnerID, source)
		if err != nil {
			return err
		}
		source.Ready = len(source.BlockedBy) == 0 && !source.Links.Paused
		out = summarize(source, func(run Execution) bool { return at != nil && run.OccurrenceAt != nil && run.OccurrenceAt.Equal(*at) })
		links, err := persistence.Select[Links](ctx, s.store, bucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID)}, Fields: []string{"Dependencies"}})
		if err != nil {
			return err
		}
		for _, link := range links {
			for _, dependency := range link.Dependencies {
				if dependency == ref {
					out.HasSuccessors = true
				}
			}
		}
		return nil
	})
	return out, err
}

type historyCursor struct {
	Owner core.ID
	Ref   core.Object
	At    time.Time
	ID    core.ID
}

func beforeHistory(a, b ExecutionSummary) bool {
	if !a.StartedAt.Equal(b.StartedAt) {
		return a.StartedAt.After(b.StartedAt)
	}
	return a.ID > b.ID
}

// History merges personal runs with projected task metadata. Bodies never enter
// this response; the cursor binds both owner and plan and uses stable tie ordering.
func (s *Coordinator) History(ctx context.Context, p core.Principal, ref core.Object, request core.PageRequest) (core.Page[ExecutionSummary], error) {
	out := core.Page[ExecutionSummary]{Items: []ExecutionSummary{}}
	page, err := request.Normalize()
	if err != nil {
		return out, err
	}
	cursor := historyCursor{Owner: p.OwnerID, Ref: ref}
	if page.Cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(page.Cursor)
		if e != nil || len(raw) > 2048 || json.Unmarshal(raw, &cursor) != nil || cursor.Owner != p.OwnerID || cursor.Ref != ref || cursor.At.IsZero() || cursor.ID == "" {
			return out, core.Invalid("cursor", "query mismatch")
		}
	}
	err = s.store.Read(ctx, func(ctx context.Context, _ persistence.Tx) error {
		if err := s.authorize(ctx, p, ref, policy.Read); err != nil {
			return err
		}
		source, err := s.source(ctx, p.OwnerID, ref)
		if err != nil {
			return err
		}
		if p.Kind == core.ActorAssistant && source.AssistantID != p.SubjectID {
			return core.NewError(core.Forbidden, "plan belongs to another assistant")
		}
		if err = s.loadListDetails(ctx, p.OwnerID, []*Plan{&source}, true); err != nil {
			return err
		}
		runs := make([]ExecutionSummary, 0, len(source.Executions))
		for _, run := range source.Executions {
			summary := executionSummary(run)
			if page.Cursor == "" || beforeHistory(ExecutionSummary{ID: cursor.ID, StartedAt: cursor.At}, summary) {
				runs = append(runs, summary)
			}
		}
		sort.Slice(runs, func(i, j int) bool { return beforeHistory(runs[i], runs[j]) })
		more := len(runs) > page.Limit
		if more {
			runs = runs[:page.Limit]
		}
		out.Items = runs
		if more {
			last := runs[len(runs)-1]
			cursor.At = last.StartedAt
			cursor.ID = last.ID
			raw, _ := json.Marshal(cursor)
			out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
		}
		return nil
	})
	return out, err
}

func (s *Coordinator) Execution(ctx context.Context, p core.Principal, ref core.Object, id core.ID) (Execution, error) {
	var out Execution
	err := s.store.Read(ctx, func(ctx context.Context, _ persistence.Tx) error {
		if err := s.authorize(ctx, p, ref, policy.Read); err != nil {
			return err
		}
		source, err := s.source(ctx, p.OwnerID, ref)
		if err != nil {
			return err
		}
		if p.Kind == core.ActorAssistant && source.AssistantID != p.SubjectID {
			return core.NewError(core.Forbidden, "plan belongs to another assistant")
		}
		for _, run := range source.Links.Runs {
			if run.ID == id {
				out = run
				return nil
			}
		}
		t, err := persistence.Load[task.Task](ctx, s.store, "business.tasks", persistence.Key(p.OwnerID, id))
		if err != nil {
			return err
		}
		if t.OwnerID != p.OwnerID || t.Plan != ref {
			return core.NewError(core.NotFound, "execution not found")
		}
		out = taskRun(t)
		return nil
	})
	return out, err
}
