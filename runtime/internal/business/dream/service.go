package dream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"sort"
	"strings"
	"sync"
	"time"
)

const configs = "business.dream_configs"
const runs = "business.dream_runs"

type Options struct {
	Authorizer    policy.Authorizer
	Memories      memory.Service
	Files         *memory.Files
	Conversations conversation.Service
	Now           func() time.Time
}
type Coordinator struct {
	store    persistence.Store
	options  Options
	mu       sync.Mutex
	lastTick time.Time
	commitMu sync.Mutex
}

func New(store persistence.Store, o Options) *Coordinator {
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Coordinator{store: store, options: o}
}
func id(parts ...string) core.ID {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return core.ID(hex.EncodeToString(h[:]))
}
func (s *Coordinator) authorize(ctx context.Context, p core.Principal, a core.ID, write bool) error {
	action := policy.Read
	if write {
		action = policy.Write
	}
	if s.options.Authorizer == nil {
		return core.NewError(core.Unavailable, "authorization required")
	}
	return s.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "assistant", ID: a}, Action: action})
}
func (s *Coordinator) config(ctx context.Context, owner, a core.ID) (Config, error) {
	return s.configAt(ctx, owner, a, s.options.Now())
}
func (s *Coordinator) configAt(ctx context.Context, owner, a core.ID, at time.Time) (Config, error) {
	c, err := persistence.Load[Config](ctx, s.store, configs, persistence.Key(owner, a))
	if err == nil {
		if c.MaxDurationSeconds == 0 {
			c.MaxDurationSeconds = 600
		}
		return c, nil
	}
	if !persistence.Missing(err) {
		return c, err
	}
	prefs, err := settings.NewRepository(s.store).Get(ctx, owner)
	if err != nil && !persistence.Missing(err) {
		return c, err
	}
	tz := prefs.Timezone
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return c, err
	}
	now := at.In(loc)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -1)
	return Config{MaxDurationSeconds: 600, OwnerID: owner, AssistantID: a, Enabled: true, Hour: 3, Timezone: tz, NextDay: day.UTC(), Version: 1}, nil
}
func (s *Coordinator) Get(ctx context.Context, p core.Principal, a core.ID) (Config, error) {
	if err := s.authorize(ctx, p, a, false); err != nil {
		return Config{}, err
	}
	return s.config(ctx, p.OwnerID, a)
}
func (s *Coordinator) Configure(ctx context.Context, p core.Principal, r ConfigureRequest) (out Config, err error) {
	if err = p.RequireOwner(); err != nil {
		return
	}
	if err = r.Mutation.Validate(true); err != nil {
		return
	}
	if _, err = time.LoadLocation(r.Timezone); err != nil {
		return out, core.Invalid("timezone", "unknown timezone")
	}
	if r.MaxDurationSeconds < 0 || r.MaxDurationSeconds > 3600 {
		return out, core.Invalid("max_duration_seconds", "must be 1 to 3600 seconds")
	}
	if r.Hour < 0 || r.Hour > 23 || r.Minute < 0 || r.Minute > 59 {
		return out, core.Invalid("time", "invalid time")
	}
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := s.authorize(ctx, p, r.AssistantID, true); err != nil {
			return err
		}
		receiptKey := persistence.Key(p.OwnerID, id("configure", string(r.AssistantID), r.Mutation.IdempotencyKey))
		var prior struct {
			Request ConfigureRequest
			Result  Config
		}
		if err := tx.Get(ctx, "business.dream_config_receipts", receiptKey, &prior); err == nil {
			if prior.Request != r {
				return core.NewError(core.Conflict, "configuration request changed")
			}
			out = prior.Result
			return nil
		} else if !persistence.Missing(err) {
			return err
		}
		c, err := s.config(ctx, p.OwnerID, r.AssistantID)
		if err != nil {
			return err
		}
		if c.Version != r.Mutation.ExpectedVersion {
			return core.NewError(core.Conflict, "dream configuration changed")
		}
		timingChanged := c.Hour != r.Hour || c.Minute != r.Minute || c.Timezone != r.Timezone
		if r.MaxDurationSeconds > 0 {
			c.MaxDurationSeconds = r.MaxDurationSeconds
		}
		c.Enabled = r.Enabled
		c.Hour = r.Hour
		c.Minute = r.Minute
		c.Timezone = r.Timezone
		c.Version++
		out = c
		if err := tx.Put(ctx, configs, persistence.Key(p.OwnerID, r.AssistantID), c); err != nil {
			return err
		}
		if timingChanged {
			if err := s.resetTimer(ctx, c, s.options.Now()); err != nil {
				return err
			}
		}
		prior.Request = r
		prior.Result = c
		return tx.Put(ctx, "business.dream_config_receipts", receiptKey, prior)
	})
	return
}
func (s *Coordinator) Runs(ctx context.Context, p core.Principal, a core.ID) ([]Run, error) {
	out := []Run{}
	err := s.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := s.authorize(ctx, p, a, false); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, runs, persistence.Query{
			Where:   []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID), persistence.Eq("AssistantID", a)},
			OrderBy: "CreatedAtKey", Descending: true, ThenBy: "key", Limit: 60,
		})
		if err != nil {
			return err
		}
		// Multiple batches share a session. Read its state once within this snapshot.
		states := map[core.ID]string{}
		for _, row := range rows {
			r, err := persistence.Decode[Run](row)
			if err != nil {
				return err
			}
			for i := range r.Sources {
				r.Sources[i].Text = ""
			}
			r.Output = ""
			if r.SessionID != "" {
				state, ok := states[r.SessionID]
				if !ok {
					var session Session
					if err := tx.Get(ctx, sessions, persistence.Key(p.OwnerID, r.SessionID), &session); err != nil {
						return err
					}
					state = session.State
					states[r.SessionID] = state
				}
				r.SessionState = state
			}
			out = append(out, r)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Coordinator) Request(ctx context.Context, p core.Principal, r Request) (out Run, err error) {
	if err = p.RequireOwner(); err != nil {
		return
	}
	if err = r.Mutation.Validate(false); err != nil {
		return
	}
	if err = s.authorize(ctx, p, r.AssistantID, true); err != nil {
		return
	}
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		key := persistence.Key(p.OwnerID, id("request", string(r.AssistantID), r.Mutation.IdempotencyKey))
		if err := tx.Get(ctx, "business.dream_requests", key, &out); err == nil {
			return nil
		} else if !persistence.Missing(err) {
			return err
		}
		c, e := s.config(ctx, p.OwnerID, r.AssistantID)
		if e != nil {
			return e
		}
		loc, e := time.LoadLocation(c.Timezone)
		if e != nil {
			return e
		}
		now := s.options.Now()
		_ = loc
		out, e = s.startSession(ctx, c, true, now, now)
		if e != nil {
			return e
		}
		return tx.Put(ctx, "business.dream_requests", key, out)
	})
	return
}

// Tick materializes at most one bounded batch per assistant, with no model I/O.
func (s *Coordinator) Tick(ctx context.Context, now time.Time) error {
	if now.IsZero() {
		return core.Invalid("now", "dispatch time required")
	}
	if err := s.expire(ctx, now); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.lastTick.IsZero() && !now.Before(s.lastTick) && now.Sub(s.lastTick) < time.Minute {
		return nil
	}
	rows, err := persistence.Select[assistant.Assistant](ctx, s.store, "business.assistants", persistence.Query{Where: []persistence.Filter{persistence.Eq("State", assistant.Active)}})
	if err != nil {
		return err
	}
	for _, a := range rows {
		if a.State != assistant.Active {
			continue
		}
		err = s.tickAssistant(ctx, a.OwnerID, a.ID, now)
		if err != nil {
			return err
		}
	}
	s.lastTick = now
	return nil
}
func (s *Coordinator) saveEvent(ctx context.Context, r Run) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, runs, persistence.Key(r.OwnerID, r.ID), r); err != nil {
			return err
		}
		if r.SessionID != "" {
			if err := tx.Put(ctx, "business.dream_run_sessions", persistence.Key(r.OwnerID, r.ID), struct{ ID, OwnerID, SessionID core.ID }{r.ID, r.OwnerID, r.SessionID}); err != nil {
				return err
			}
		}
		return event.NewWriter(s.store).AppendAssistant(ctx, event.AssistantEvent{ID: r.EventID, OwnerID: r.OwnerID, AssistantID: r.AssistantID, Kind: event.DreamDue, DeliveryMode: event.WhenIdle, OperationID: r.ID, OccurrenceAt: r.End, CreatedAt: r.UpdatedAt})
	})
}
func (s *Coordinator) collect(ctx context.Context, c Config, a assistant.Assistant, start, end time.Time) ([]Evidence, error) {
	messages, err := persistence.Select[conversation.Message](ctx, s.store, "im.messages", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerKey", c.OwnerID), persistence.Eq("ConversationID", a.ConversationID), {Field: "CreatedAtKey", Op: ">=", Value: persistence.TimeKey(start)}, {Field: "CreatedAtKey", Op: "<", Value: persistence.TimeKey(end)}}})
	if err != nil {
		return nil, err
	}
	out := []Evidence{}
	for _, m := range messages {
		if m.ConversationID != a.ConversationID || m.CreatedAt.Before(start) || !m.CreatedAt.Before(end) {
			continue
		}
		text := m.Content.Text
		if m.Content.Card != nil {
			text = m.Content.Card.Title + "\n" + m.Content.Card.Summary
		}
		if strings.TrimSpace(text) == "" || sensitive(text) {
			continue
		}
		ref := core.Object{Kind: "message", ID: m.ID}
		if sensitive(text) {
			continue
		}
		blocked, err := s.options.Files.Suppressed(ctx, memory.Memory{OwnerID: c.OwnerID, AssistantID: a.ID, Sources: []core.Object{ref}, Text: text})
		if err != nil {
			return nil, err
		}
		if blocked {
			continue
		}
		out = append(out, Evidence{ref, string(id(text)), m.CreatedAt, string(m.Sender), text})
	}
	tasks, err := persistence.Select[task.Task](ctx, s.store, "business.tasks", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", c.OwnerID), persistence.Eq("AssistantID", a.ID), {Field: "UpdatedAtKey", Op: ">=", Value: persistence.TimeKey(start)}, {Field: "UpdatedAtKey", Op: "<", Value: persistence.TimeKey(end)}}})
	if err != nil {
		return nil, err
	}
	for _, t := range tasks {
		if t.OwnerID != c.OwnerID || t.AssistantID != a.ID || t.UpdatedAt.Before(start) || !t.UpdatedAt.Before(end) {
			continue
		}
		ref := core.Object{Kind: "task", ID: t.ID}
		text := fmt.Sprintf("Goal: %s\nState: %s\nArtifacts: %v\nReason: %s", t.Goal, t.State, t.ArtifactIDs, t.WaitingReason)
		if sensitive(text) {
			continue
		}
		blocked, err := s.options.Files.Suppressed(ctx, memory.Memory{OwnerID: c.OwnerID, AssistantID: a.ID, Sources: []core.Object{ref}, Text: text})
		if err != nil {
			return nil, err
		}
		if blocked {
			continue
		}
		out = append(out, Evidence{ref, fmt.Sprint(t.Version), t.UpdatedAt, "task_" + string(t.State), text})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].At.Equal(out[j].At) {
			return out[i].Source.ID < out[j].Source.ID
		}
		return out[i].At.Before(out[j].At)
	})
	return out, nil
}
