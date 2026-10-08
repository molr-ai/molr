package debugtrace

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// Trajectory keeps payloads behind Detail. Cursors survive retention and are
// bound to the owner and assistant filter, never used as authorization.
type TrajectoryQuery struct {
	Query
	After string
}
type TrajectoryEntry struct {
	RunID core.ID
	Step  Step
}
type TrajectoryPage struct {
	Items          []TrajectoryEntry
	Runs           []Run
	NextCursor     string
	HeadCursor     string
	ContextResetAt *time.Time
	Cleanup        *CleanupEvent
}
type trajectoryCursor struct {
	Owner, Assistant, Run core.ID
	Revision              uint64
	At                    time.Time
	Sequence              int
}

func encodeTrajectoryCursor(c trajectoryCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}
func compareTrajectory(a, b trajectoryCursor) int {
	if a.Revision != b.Revision {
		if a.Revision < b.Revision {
			return -1
		}
		return 1
	}
	if !a.At.Equal(b.At) {
		if a.At.Before(b.At) {
			return -1
		}
		return 1
	}
	if a.Run != b.Run {
		return strings.Compare(string(a.Run), string(b.Run))
	}
	if a.Sequence < b.Sequence {
		return -1
	}
	if a.Sequence > b.Sequence {
		return 1
	}
	return 0
}
func (s *Service) Trajectory(ctx context.Context, p core.Principal, q TrajectoryQuery) (out TrajectoryPage, err error) {
	if err = s.authorize(ctx, p); err != nil {
		return
	}
	page, err := q.Page.Normalize()
	if err != nil {
		return out, err
	}
	if q.AssistantID != "" {
		if err = q.AssistantID.Validate(); err != nil {
			return out, err
		}
	}
	if len(q.After) > 2048 || (q.After != "" && page.Cursor != "") {
		return out, core.Invalid("cursor", "use either cursor or after")
	}
	raw := page.Cursor
	if q.After != "" {
		raw = q.After
	}
	var boundary trajectoryCursor
	if raw != "" {
		b, e := base64.RawURLEncoding.DecodeString(raw)
		if e != nil || json.Unmarshal(b, &boundary) != nil || boundary.Owner != p.OwnerID || boundary.Assistant != q.AssistantID || boundary.Sequence < 0 {
			return out, core.Invalid("cursor", "invalid trajectory cursor")
		}
	}
	cursorFor := func(e TrajectoryEntry) trajectoryCursor {
		return trajectoryCursor{p.OwnerID, q.AssistantID, e.RunID, e.Step.Revision, e.Step.At, e.Step.Sequence}
	}
	out.Items = []TrajectoryEntry{}
	out.Runs = []Run{}
	err = s.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		policy, err := s.retention(ctx, tx.Store())
		if err != nil {
			return err
		}
		protected, err := s.protected(ctx, tx.Store(), p.OwnerID, policy)
		if err != nil {
			return err
		}
		query := visibleQuery(p.OwnerID, protected, persistence.TimeKey(s.options.Now().AddDate(0, 0, -policy.RetentionDays)))
		if q.AssistantID != "" {
			query.Where = append(query.Where, persistence.Eq("AssistantID", q.AssistantID))
		}
		summaries, err := persistence.Select[Run](ctx, tx.Store(), "debug.summaries", query)
		if err != nil {
			return err
		}
		entries := []TrajectoryEntry{}
		// Inspect newest candidates first; older runs cannot contribute to a filled page.
		sort.Slice(summaries, func(i, j int) bool { return summaries[i].LastRevision > summaries[j].LastRevision })
		for _, summary := range summaries {
			if summary.OwnerID != p.OwnerID || (q.AssistantID != "" && summary.AssistantID != q.AssistantID) || !visibleRun(summary, policy, protected, s.options.Now()) {
				continue
			}
			summary.Steps = nil
			if summary.Source == nil {
				summary.Source = sourceSummary(s.sourceForRun(ctx, tx, summary))
			}
			out.Runs = append(out.Runs, summary)
			// A live refresh reads only runs with new steps. Legacy summaries have
			// zero revisions and deliberately take the compatibility path below.
			if q.After != "" && summary.LastRevision > 0 && summary.LastRevision <= boundary.Revision {
				continue
			}
			if page.Cursor != "" && summary.FirstRevision > 0 && summary.FirstRevision >= boundary.Revision {
				continue
			}
			if q.After == "" && summary.LastRevision > 0 && len(entries) > page.Limit {
				sort.Slice(entries, func(i, j int) bool { return compareTrajectory(cursorFor(entries[i]), cursorFor(entries[j])) > 0 })
				entries = entries[:page.Limit+1]
				if entries[page.Limit].Step.Revision > 0 && summary.LastRevision < entries[page.Limit].Step.Revision {
					continue
				}
			}
			var run Run
			if err := tx.Get(ctx, "debug.runs", persistence.Key(p.OwnerID, summary.ID), &run); err != nil {
				return err
			}
			for _, step := range run.Steps {
				e := TrajectoryEntry{run.ID, step}
				order := compareTrajectory(cursorFor(e), boundary)
				if raw == "" || (q.After != "" && order > 0) || (page.Cursor != "" && order < 0) {
					entries = append(entries, e)
				}
			}
		}
		sort.Slice(out.Runs, func(i, j int) bool {
			if out.Runs[i].FirstRevision != out.Runs[j].FirstRevision {
				return out.Runs[i].FirstRevision < out.Runs[j].FirstRevision
			}
			if out.Runs[i].StartedAt.Equal(out.Runs[j].StartedAt) {
				return out.Runs[i].ID < out.Runs[j].ID
			}
			return out.Runs[i].StartedAt.Before(out.Runs[j].StartedAt)
		})
		sort.Slice(entries, func(i, j int) bool { return compareTrajectory(cursorFor(entries[i]), cursorFor(entries[j])) < 0 })
		more := len(entries) > page.Limit
		if more {
			if q.After != "" {
				entries = entries[:page.Limit]
			} else {
				entries = entries[len(entries)-page.Limit:]
			}
		}
		out.Items = entries
		if len(entries) > 0 {
			out.HeadCursor = encodeTrajectoryCursor(cursorFor(entries[len(entries)-1]))
			if more {
				anchor := entries[0]
				if q.After != "" {
					anchor = entries[len(entries)-1]
				}
				out.NextCursor = encodeTrajectoryCursor(cursorFor(anchor))
			}
		} else if q.After != "" {
			out.HeadCursor = q.After
		} else {
			var revision uint64
			if err := tx.Get(ctx, "debug.revisions", string(p.OwnerID), &revision); err != nil && !persistence.Missing(err) {
				return err
			}
			out.HeadCursor = encodeTrajectoryCursor(trajectoryCursor{Owner: p.OwnerID, Assistant: q.AssistantID, Revision: revision, At: s.options.Now().UTC()})
		}
		// Old captures predate metadata previews. Hydrate only this bounded page,
		// from already-sanitized saved payloads; never fetch provider data again.
		for i := range out.Items {
			e := &out.Items[i]
			if e.Step.Revision != 0 {
				continue
			}
			var payload Payload
			if err := tx.Get(ctx, "debug.payloads", payloadKey(p.OwnerID, e.RunID, e.Step.Sequence), &payload); err != nil {
				if persistence.Missing(err) {
					continue
				}
				return err
			}
			e.Step.Name, e.Step.Preview = trajectoryPreview(e.Step.Kind, payload.Data)
			e.Step.Summary = toolSummary(e.Step.Kind, e.Step.State, payload.Data)
		}
		if q.AssistantID != "" {
			var cleanup CleanupEvent
			if err := tx.Get(ctx, "debug.cleanup", persistence.Key(p.OwnerID, q.AssistantID), &cleanup); err == nil {
				out.Cleanup = &cleanup
			} else if !persistence.Missing(err) {
				return err
			}
			var reset time.Time
			if err := tx.Get(ctx, "agent.context_reset_at", persistence.Key(p.OwnerID, q.AssistantID), &reset); err == nil {
				out.ContextResetAt = &reset
			} else if !persistence.Missing(err) {
				return err
			}
		}
		return nil
	})
	return
}

func shortPreview(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	r := []rune(value)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return value
}
func trajectoryPreview(kind string, encoded json.RawMessage) (name, preview string) {
	var data map[string]json.RawMessage
	if json.Unmarshal(encoded, &data) != nil {
		return
	}
	_ = json.Unmarshal(data["Name"], &name)
	name = shortPreview(name, 80)
	var value json.RawMessage
	switch kind {
	case "model.start":
		value = data["ModelID"]
	case "model.complete":
		value = data["Text"]
		if len(value) == 0 {
			value = data["Reason"]
		}
	case "model.retry", "model.retry_exhausted":
		value = data["Reason"]
	case "tool.start":
		value = data["Arguments"]
	case "tool.complete":
		var result map[string]json.RawMessage
		if json.Unmarshal(data["Result"], &result) == nil {
			value = result["Content"]
		} else {
			value = data["Result"]
		}
		if len(value) == 0 {
			value = data["Reason"]
		}
	case "run.start":
		value = data["Event"]
	case "run.end":
		value = data["Reason"]
	}
	var plain string
	if json.Unmarshal(value, &plain) == nil {
		preview = plain
	} else if len(value) > 0 {
		preview = string(value)
	}
	return name, shortPreview(preview, 240)
}
