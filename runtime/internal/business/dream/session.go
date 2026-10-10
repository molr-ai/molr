package dream

import (
	"context"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/trigger"
	"sort"
	"time"
)

const sessions = "business.dream_sessions"
const backlogs = "business.dream_backlogs"

// A session owns the assistant until completion/deadline. Its bounded batches
// have independent model contexts but share this durable, immutable deadline.
type Session struct {
	ID, OwnerID, AssistantID, CurrentRunID core.ID
	State, Timezone                        string
	WindowStart, Deadline, Start, End      time.Time
	Manual                                 bool
	Started                                bool
	Sources, Deferred                      []Evidence
	FreshCount, Offset, Batch, Unprocessed int
	Retries                                int
	MemoryFiles                            []memory.Ref
	FinishedAt                             *time.Time
}
type backlog struct {
	OwnerID, AssistantID core.ID
	EligibleOn           time.Time
	Sources              []Evidence
}

func evidenceKey(e Evidence) string {
	return e.Source.Kind + "/" + string(e.Source.ID) + "/" + e.Version
}
func takeEvidence(in []Evidence, limit, bytes int) ([]Evidence, []Evidence) {
	n, size := 0, 0
	for n < len(in) && n < limit {
		if size+len(in[n].Text) > bytes {
			break
		}
		size += len(in[n].Text)
		n++
	}
	return append([]Evidence{}, in[:n]...), in[n:]
}
func (s *Coordinator) startSession(ctx context.Context, c Config, manual bool, now, at time.Time) (Run, error) {
	a, err := assistant.NewRepository(s.store).Get(ctx, c.OwnerID, c.AssistantID)
	if err != nil {
		return Run{}, err
	}
	if a.State != assistant.Active {
		return Run{}, core.NewError(core.FailedPrecondition, "assistant inactive")
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return Run{}, err
	}
	end := dayStart(at, loc)
	start := end.AddDate(0, 0, -1)
	all, err := persistence.Select[Session](ctx, s.store, sessions, persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", c.OwnerID), persistence.Eq("AssistantID", c.AssistantID)}, Any: [][]persistence.Filter{{persistence.Eq("State", "active"), {Field: "DeadlineKey", Op: ">", Value: persistence.TimeKey(now)}}, {persistence.Eq("EndKey", persistence.TimeKey(end))}}})
	if err != nil {
		return Run{}, err
	}
	for _, v := range all {
		if v.OwnerID != c.OwnerID || v.AssistantID != c.AssistantID {
			continue
		}
		if manual && v.State == "missed_window" {
			continue // A never-started automatic window does not consume a manual run.
		}
		if v.State == "active" && now.Before(v.Deadline) || v.End.Equal(end) {
			r, err := persistence.Load[Run](ctx, s.store, runs, persistence.Key(c.OwnerID, v.CurrentRunID))
			if persistence.Missing(err) {
				return Run{AssistantID: c.AssistantID, SessionID: v.ID, State: v.State, Deadline: v.Deadline}, nil
			}
			if manual && v.State == "failed" && v.Retries < 1 && now.Before(v.Deadline) {
				v.State = "active"
				v.FinishedAt = nil
				v.Retries++
				v.Started = false
				v.Unprocessed -= len(v.Sources) - v.Offset
				r.State = "queued"
				r.FailureCode = ""
				r.UpdatedAt = now.UTC()
				r.EventID = id(string(r.ID), "retry", now.UTC().String())
				err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
					if err := saveSession(ctx, tx, v); err != nil {
						return err
					}
					return s.saveEvent(ctx, r)
				})
				return r, err
			}
			r.SessionState = v.State
			if v.State == "completed" || v.State == "skipped" {
				r.State = "up_to_date"
			}
			return r, err
		}
	}
	duration := c.MaxDurationSeconds
	if duration == 0 {
		duration = 600
	}
	v := Session{ID: id(string(c.AssistantID), end.UTC().String(), "session"), OwnerID: c.OwnerID, AssistantID: c.AssistantID, State: "active", Timezone: c.Timezone, WindowStart: at.UTC(), Deadline: at.Add(time.Duration(duration) * time.Second).UTC(), Start: start.UTC(), End: end.UTC(), Manual: manual, MemoryFiles: []memory.Ref{}}
	if manual {
		v.ID = id(string(v.ID), "manual")
	}
	if !now.Before(v.Deadline) {
		v.State = "missed_window"
		v.FinishedAt = &now
		return s.emptySession(ctx, v, now)
	}
	fresh, err := s.collect(ctx, c, a, start, end)
	if err != nil {
		return Run{}, err
	}
	old, err := persistence.Load[backlog](ctx, s.store, backlogs, persistence.Key(c.OwnerID, c.AssistantID))
	if err != nil && !persistence.Missing(err) {
		return Run{}, err
	}
	seen := map[string]bool{}
	candidates := append(append([]Evidence{}, fresh...), old.Sources...)
	for _, src := range candidates {
		key := evidenceKey(src)
		if _, checked := seen[key]; checked {
			continue
		}
		prior, err := persistence.Select[Run](ctx, s.store, runs, persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", c.OwnerID), persistence.Eq("AssistantID", c.AssistantID), {Field: "State", Op: "in", Value: []string{"completed", "undone"}}, {Field: "Sources.Evidence", Op: "has", Value: key}}, Limit: 1, Fields: []string{"ID"}})
		if err != nil {
			return Run{}, err
		}
		seen[key] = len(prior) > 0
	}
	filtered := []Evidence{}
	for _, src := range fresh {
		if !seen[evidenceKey(src)] {
			filtered = append(filtered, src)
		}
	}
	chosen, remaining := takeEvidence(filtered, 320, 384<<10)
	v.Sources = chosen
	v.FreshCount = len(chosen)
	v.Deferred, _ = takeEvidence(remaining, 80, 96<<10)
	v.Unprocessed = len(remaining)
	if old.EligibleOn.Equal(end) {
		carry := []Evidence{}
		for _, src := range old.Sources {
			if !seen[evidenceKey(src)] {
				carry = append(carry, src)
			}
		}
		sort.SliceStable(carry, func(i, j int) bool { return carry[i].At.Before(carry[j].At) })
		carry, _ = takeEvidence(carry, 80, 96<<10)
		// Older evidence is processed before the fresh suffix.
		v.Sources = append(carry, v.Sources...)
	}
	maintenance := false
	if len(v.Sources) == 0 {
		// Scheduling has no Agent execution identity yet. Read only this
		// already-authorized assistant's file metadata through the local store.
		page, err := s.options.Files.List(ctx, c.OwnerID, memory.Query{Scope: memory.Assistant, AssistantID: c.AssistantID, Page: core.PageRequest{Limit: 1}})
		if err != nil {
			return Run{}, err
		}
		for _, m := range page.Items {
			if m.Path != "" {
				maintenance = true
			}
		}
	}

	if len(v.Sources) == 0 && !maintenance {
		v.State = "skipped"
		v.FinishedAt = &now
		return s.emptySession(ctx, v, now)
	}
	r := s.batch(v, now)
	v.CurrentRunID = r.ID
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := saveSession(ctx, tx, v); err != nil {
			return err
		}
		return s.saveEvent(ctx, r)
	})
	return r, err
}
func (s *Coordinator) batch(v Session, now time.Time) Run {
	end := len(v.Sources)
	freshStart := len(v.Sources) - v.FreshCount
	backfill := v.Offset < freshStart
	if backfill {
		end = freshStart
	}
	loc := mustLocation(v.Timezone)
	start, until := v.Start, v.End
	if v.Offset < end {
		start = dayStart(v.Sources[v.Offset].At, loc).UTC()
		until = start.In(loc).AddDate(0, 0, 1).UTC()
		// Finish every batch for this local date before advancing to the next.
		for i := v.Offset + 1; i < end; i++ {
			if !v.Sources[i].At.Before(until) {
				end = i
				break
			}
		}
	}
	sources, _ := takeEvidence(v.Sources[v.Offset:end], 80, 96<<10)
	runID := id(string(v.ID), "batch", time.Duration(v.Batch).String())
	return Run{ID: runID, EventID: id(string(runID), "event"), SessionID: v.ID, Deadline: v.Deadline, Batch: v.Batch + 1, Maintenance: len(v.Sources) == 0, Backfill: backfill, OwnerID: v.OwnerID, AssistantID: v.AssistantID, Manual: v.Manual, Day: start.In(loc).Format("2006-01-02"), Timezone: v.Timezone, Start: start, End: until, State: "queued", Sources: sources, MemoryFiles: []memory.Ref{}, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
}
func mustLocation(zone string) *time.Location {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.UTC
	}
	return loc
}
func (s *Coordinator) emptySession(ctx context.Context, v Session, now time.Time) (Run, error) {
	r := Run{ID: id(string(v.ID), "empty"), SessionID: v.ID, Deadline: v.Deadline, OwnerID: v.OwnerID, AssistantID: v.AssistantID, Manual: v.Manual, Day: v.Start.In(mustLocation(v.Timezone)).Format("2006-01-02"), Timezone: v.Timezone, Start: v.Start, End: v.End, State: v.State, Sources: []Evidence{}, MemoryFiles: []memory.Ref{}, CreatedAt: now, UpdatedAt: now}
	v.CurrentRunID = r.ID
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, runs, persistence.Key(v.OwnerID, r.ID), r); err != nil {
			return err
		}
		return saveSession(ctx, tx, v)
	})
	return r, err
}
func (s *Coordinator) finishSession(ctx context.Context, tx persistence.Tx, v Session, state string, now time.Time) error {
	v.State = state
	v.FinishedAt = &now
	left := []Evidence{}
	freshOffset := max(v.Offset, len(v.Sources)-v.FreshCount)
	if freshOffset < len(v.Sources) {
		left = append(left, v.Sources[freshOffset:]...)
	}
	left = append(left, v.Deferred...)
	left, _ = takeEvidence(left, 80, 96<<10)
	// Only fresh sources get one automatic retry; older backfill is never carried again.
	if err := tx.Put(ctx, backlogs, persistence.Key(v.OwnerID, v.AssistantID), backlog{OwnerID: v.OwnerID, AssistantID: v.AssistantID, EligibleOn: v.End.In(mustLocation(v.Timezone)).AddDate(0, 0, 1).UTC(), Sources: left}); err != nil {
		return err
	}
	c, err := s.config(ctx, v.OwnerID, v.AssistantID)
	if err != nil {
		return err
	}
	if c.NextDay.Before(v.End) {
		c.NextDay = v.End
		c.Version++
		if err := tx.Put(ctx, configs, persistence.Key(v.OwnerID, v.AssistantID), c); err != nil {
			return err
		}
	}
	v.Unprocessed += len(v.Sources) - v.Offset
	return saveSession(ctx, tx, v)
}
func (s *Coordinator) advanceSession(ctx context.Context, r Run) error {
	if r.SessionID == "" {
		return nil
	}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		v, err := persistence.Load[Session](ctx, s.store, sessions, persistence.Key(r.OwnerID, r.SessionID))
		if err != nil {
			return err
		}
		if v.State != "active" || !s.options.Now().Before(v.Deadline) {
			return core.NewError(core.DeadlineExceeded, "dream deadline reached")
		}
		v.Offset += len(r.Sources)
		v.Batch++
		v.MemoryFiles = append(v.MemoryFiles, r.MemoryFiles...)
		if v.Offset == len(v.Sources) {
			state := "completed"
			if v.Unprocessed > 0 {
				state = "partial"
			}
			return s.finishSession(ctx, tx, v, state, s.options.Now().UTC())
		}
		next := s.batch(v, s.options.Now())
		v.CurrentRunID = next.ID
		if err := saveSession(ctx, tx, v); err != nil {
			return err
		}
		return s.saveEvent(ctx, next)
	})
}

// Deadline is checked before preparation, during execution and at commit. It
// also upgrades any legacy queued batch without granting a fresh time budget.
func (s *Coordinator) Deadline(ctx context.Context, p core.Principal, e event.AssistantEvent) (time.Time, error) {
	r, err := s.run(ctx, p, e)
	if err != nil {
		return time.Time{}, err
	}
	if r.State == "completed" {
		return time.Time{}, nil
	}
	if r.Deadline.IsZero() {
		r.Deadline = r.CreatedAt.Add(10 * time.Minute)
	}
	return r.Deadline, nil
}
func (s *Coordinator) Active(ctx context.Context, p core.Principal, e event.AssistantEvent) (bool, error) {
	r, err := persistence.Load[Run](ctx, s.store, runs, persistence.Key(p.OwnerID, e.OperationID))
	if err != nil {
		return false, err
	}
	if r.SessionID == "" {
		return false, nil
	}
	v, err := persistence.Load[Session](ctx, s.store, sessions, persistence.Key(p.OwnerID, r.SessionID))
	return err == nil && v.State == "active" && s.options.Now().Before(v.Deadline), err
}
func (s *Coordinator) expire(ctx context.Context, now time.Time) error {
	for {
		due, err := persistence.Select[Session](ctx, s.store, "business.dream_sessions_index", persistence.Query{
			Where: []persistence.Filter{persistence.Eq("State", "active"),
				{Field: "DeadlineKey", Op: "<=", Value: persistence.TimeKey(now)}},
			OrderBy: "DeadlineKey", Limit: 100,
		})
		if err != nil {
			return err
		}
		for _, v := range due {
			state := "timed_out"
			if !v.Started {
				state = "missed_window"
			}
			if err := s.closeSession(ctx, v.OwnerID, v.ID, state, now); err != nil {
				return err
			}
		}
		if len(due) < 100 {
			return nil
		}
	}
}

func (s *Coordinator) closeSession(ctx context.Context, owner, id core.ID, state string, now time.Time) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		v, err := persistence.Load[Session](ctx, s.store, sessions, persistence.Key(owner, id))
		if err != nil {
			return err
		}
		if v.State != "active" {
			return nil
		}
		r, err := persistence.Load[Run](ctx, s.store, runs, persistence.Key(owner, v.CurrentRunID))
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if err == nil && (r.State == "queued" || r.State == "running") {
			r.State = state
			r.FailureCode = state
			r.UpdatedAt = now
			if err := tx.Put(ctx, runs, persistence.Key(owner, r.ID), r); err != nil {
				return err
			}
		}
		return s.finishSession(ctx, tx, v, state, now)
	})
}

// Cancel stops only the dream session, preserving ordinary queued messages.
func (s *Coordinator) Cancel(ctx context.Context, p core.Principal, a core.ID) error {
	if err := p.RequireOwner(); err != nil {
		return err
	}
	if err := s.authorize(ctx, p, a, true); err != nil {
		return err
	}
	all, err := persistence.Select[Session](ctx, s.store, sessions, persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID), persistence.Eq("AssistantID", a), persistence.Eq("State", "active")}})
	if err != nil {
		return err
	}
	for _, v := range all {
		if v.OwnerID == p.OwnerID && v.AssistantID == a && v.State == "active" {
			if err := s.closeSession(ctx, p.OwnerID, v.ID, "canceled", s.options.Now().UTC()); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Coordinator) Windows(ctx context.Context, p core.Principal, a core.ID, from, to time.Time) ([]Window, error) {
	if err := p.RequireOwner(); err != nil {
		return nil, err
	}
	if from.IsZero() || !to.After(from) || to.Sub(from) > 125*24*time.Hour {
		return nil, core.Invalid("range", "maximum range is 125 days")
	}
	repo := assistant.NewRepository(s.store)
	query := assistant.Query{State: assistant.Active, Page: core.PageRequest{Limit: 200}}
	out := []Window{}
	for {
		var people core.Page[assistant.Assistant]
		if a != "" {
			person, err := repo.Get(ctx, p.OwnerID, a)
			if err != nil {
				return nil, err
			}
			people.Items = []assistant.Assistant{person}
		} else {
			var err error
			people, err = repo.List(ctx, p.OwnerID, query)
			if err != nil {
				return nil, err
			}
		}
		for _, person := range people.Items {
			if person.State != assistant.Active {
				continue
			}
			if err := s.authorize(ctx, p, person.ID, false); err != nil {
				return nil, err
			}
			c, err := s.config(ctx, p.OwnerID, person.ID)
			if err != nil {
				return nil, err
			}
			if !c.Enabled {
				continue
			}
			loc, err := time.LoadLocation(c.Timezone)
			if err != nil {
				return nil, err
			}
			timer, err := newTimer(c, dayStart(from, loc).AddDate(0, 0, -1), timerState{})
			if err != nil {
				return nil, err
			}
			ats, err := trigger.Between(ctx, timer.Rule, from, to, time.Duration(c.MaxDurationSeconds)*time.Second)
			if err != nil {
				return nil, err
			}
			for _, at := range ats {
				out = append(out, Window{AssistantID: person.ID, Start: at, End: at.Add(time.Duration(c.MaxDurationSeconds) * time.Second), Timezone: c.Timezone, State: "reserved"})
			}
		}
		if a != "" || people.NextCursor == "" {
			break
		}
		query.Page.Cursor = people.NextCursor
	}
	filters := []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID), {Field: "DeadlineKey", Op: ">", Value: persistence.TimeKey(from)}, {Field: "WindowStartKey", Op: "<", Value: persistence.TimeKey(to)}}
	if a != "" {
		filters = append(filters, persistence.Eq("AssistantID", a))
	}
	all, err := persistence.Select[Session](ctx, s.store, sessions, persistence.Query{Where: filters})
	if err != nil {
		return nil, err
	}
	for _, v := range all {
		if v.OwnerID != p.OwnerID || a != "" && a != v.AssistantID || !v.WindowStart.Before(to) || !v.Deadline.After(from) {
			continue
		}
		if err := s.authorize(ctx, p, v.AssistantID, false); err != nil {
			return nil, err
		}
		w := Window{AssistantID: v.AssistantID, Start: v.WindowStart, End: v.Deadline, Timezone: v.Timezone, State: v.State, Manual: v.Manual, SessionID: v.ID}
		replaced := false
		for i, old := range out {
			if !v.Manual && old.AssistantID == v.AssistantID && old.Start.Equal(v.WindowStart) {
				out[i] = w
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

func (s *Coordinator) Conflicts(ctx context.Context, p core.Principal, a core.ID, rule trigger.Rule, from, to time.Time) ([]Window, error) {
	windows, err := s.Windows(ctx, p, a, from, to)
	if err != nil {
		return nil, err
	}
	ats, err := trigger.Between(ctx, rule, from, to, 0)
	if err != nil {
		return nil, err
	}
	out := []Window{}
	for _, w := range windows {
		if w.State == "missed_window" || w.State == "canceled" || w.State == "completed" || w.State == "skipped" || w.State == "timed_out" {
			continue
		}
		for _, at := range ats {
			if !at.Before(w.Start) && at.Before(w.End) {
				out = append(out, w)
				break
			}
		}
	}
	return out, nil
}

func (s *Coordinator) GetRun(ctx context.Context, p core.Principal, id core.ID) (Run, error) {
	if err := p.RequireOwner(); err != nil {
		return Run{}, err
	}
	r, err := persistence.Load[Run](ctx, s.store, runs, persistence.Key(p.OwnerID, id))
	if err != nil {
		return r, err
	}
	if err := s.authorize(ctx, p, r.AssistantID, false); err != nil {
		return Run{}, err
	}
	for i := range r.Sources {
		r.Sources[i].Text = ""
	}
	r.Output = ""
	if r.SessionID != "" {
		v, err := persistence.Load[Session](ctx, s.store, sessions, persistence.Key(p.OwnerID, r.SessionID))
		if err != nil {
			return r, err
		}
		r.SessionState = v.State
	}
	return r, nil
}

// Queue reads these small projections; retained evidence never needs scanning
// on each claim. The full session and projection commit atomically.
func saveSession(ctx context.Context, tx persistence.Tx, v Session) error {
	if err := tx.Put(ctx, sessions, persistence.Key(v.OwnerID, v.ID), v); err != nil {
		return err
	}
	if err := tx.Put(ctx, "business.dream_sessions_index", persistence.Key(v.OwnerID, v.ID), struct {
		ID, OwnerID, AssistantID core.ID
		State                    string
		Deadline                 time.Time
		DeadlineKey              string
		Started                  bool
	}{v.ID, v.OwnerID, v.AssistantID, v.State, v.Deadline, persistence.TimeKey(v.Deadline), v.Started}); err != nil {
		return err
	}
	return tx.NotifyAfterCommit(ctx, event.DispatchSignal())
}
