package plan

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/trigger"
	"sort"
	"time"
)

type Event struct {
	Key  string
	At   time.Time
	Plan Plan
}
type EventQuery struct {
	From               time.Time
	Scope, Kind, State string
	Page               core.PageRequest
}
type eventCursor struct {
	Owner              core.ID
	From               time.Time
	Scope, Kind, State string
	At                 time.Time
	Ref                string
}

// Events returns the next occurrences across all matching plans, including sparse
// recurrences beyond the month view. It never creates or starts executions.
func (s *Coordinator) Events(ctx context.Context, p core.Principal, q EventQuery) (core.Page[Event], error) {
	out := core.Page[Event]{Items: []Event{}}
	page, err := q.Page.Normalize()
	if err != nil {
		return out, err
	}
	if q.From.IsZero() {
		return out, core.Invalid("from", "start time required")
	}
	switch q.Kind {
	case "", "all", "item", "event", "automatic":
	default:
		return out, core.Invalid("kind", "invalid plan kind")
	}
	switch q.State {
	case "", "all", "open", "done":
	default:
		return out, core.Invalid("state", "invalid event state")
	}
	cursor := eventCursor{Owner: p.OwnerID, From: q.From, Scope: q.Scope, Kind: q.Kind, State: q.State}
	if page.Cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(page.Cursor)
		var c eventCursor
		if e != nil || len(raw) > 2048 || json.Unmarshal(raw, &c) != nil || c.Owner != p.OwnerID || !c.From.Equal(q.From) || c.Scope != q.Scope || c.Kind != q.Kind || c.State != q.State || c.At.Before(q.From) {
			return out, core.Invalid("cursor", "cursor query mismatch")
		}
		cursor = c
	}
	plans, err := s.List(ctx, p, time.Time{}, time.Time{})
	if err != nil {
		return out, err
	}
	for _, plan := range plans {
		if q.Scope == "me" && plan.AssistantID != "" || q.Scope != "" && q.Scope != "all" && q.Scope != "me" && string(plan.AssistantID) != q.Scope {
			continue
		}
		kind := "item"
		if plan.Schedule != nil {
			kind = "event"
			if plan.Schedule.Kind == "assistant_plan" {
				kind = "automatic"
			}
		}
		if q.Kind != "" && q.Kind != "all" && q.Kind != kind {
			continue
		}
		ref := plan.Ref.Kind + "/" + string(plan.Ref.ID)
		count := 0
		appendAt := func(at time.Time) bool {
			if at.Before(q.From) || !cursor.At.IsZero() && (at.Before(cursor.At) || at.Equal(cursor.At) && ref <= cursor.Ref) {
				return true
			}
			done := plan.Todo != nil && plan.Todo.Completed
			for _, run := range plan.Executions {
				if plan.Schedule == nil || plan.Schedule.Trigger.Recurrence == "" || run.OccurrenceAt != nil && run.OccurrenceAt.Equal(at) {
					done = run.State == "completed"
					break
				}
			}
			if q.State == "done" && !done || q.State == "open" && done {
				return true
			}
			out.Items = append(out.Items, Event{Key: ref + "@" + at.UTC().Format(time.RFC3339Nano), At: at.UTC(), Plan: plan})
			count++
			return count <= page.Limit
		}
		if plan.Todo != nil {
			at := plan.Todo.StartAt
			if at == nil {
				at = plan.Todo.DueAt
			}
			if at != nil {
				appendAt(*at)
			}
			continue
		}
		// Completed filtering has a finite source: recorded occurrences, not an
		// unbounded search through future recurrence dates that can never match.
		if q.State == "done" && plan.Schedule.Trigger.Recurrence != "" {
			seen := map[time.Time]bool{}
			for _, run := range plan.Executions {
				if run.OccurrenceAt != nil && run.State == "completed" && !seen[run.OccurrenceAt.UTC()] {
					seen[run.OccurrenceAt.UTC()] = true
					appendAt(*run.OccurrenceAt)
				}
			}
			continue
		}
		from := q.From
		if cursor.At.After(from) {
			from = cursor.At
		}
		if err := trigger.VisitUpcoming(ctx, trigger.Rule(plan.Schedule.Trigger), from, appendAt); err != nil {
			return out, err
		}
	}
	sort.Slice(out.Items, func(i, j int) bool {
		a, b := out.Items[i], out.Items[j]
		if !a.At.Equal(b.At) {
			return a.At.Before(b.At)
		}
		return a.Plan.Ref.Kind+"/"+string(a.Plan.Ref.ID) < b.Plan.Ref.Kind+"/"+string(b.Plan.Ref.ID)
	})
	if len(out.Items) > page.Limit {
		out.Items = out.Items[:page.Limit]
		last := out.Items[len(out.Items)-1]
		cursor.At = last.At
		cursor.Ref = last.Plan.Ref.Kind + "/" + string(last.Plan.Ref.ID)
		raw, _ := json.Marshal(cursor)
		out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, nil
}
