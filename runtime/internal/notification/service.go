// Package notification owns durable, owner-scoped notices and their acknowledgement.
package notification

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

const bucket = "notifications.records"
const sources = "notifications.sources"

type Notice struct {
	ID          core.ID
	OwnerID     core.ID
	Kind        string
	Code        string
	Source      core.Object
	AssistantID core.ID
	Label       string
	Severity    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ReadAt      *time.Time
	DismissedAt *time.Time
	ResolvedAt  *time.Time
	Version     core.Version
}

// Input contains only a safe reason code and display name, never raw errors or secrets.
// Episode distinguishes separate occurrences when the business object is reused.
type Input struct {
	Kind        string
	Code        string
	Source      core.Object
	AssistantID core.ID
	Label       string
	Severity    string
	Episode     string
}
type sourceRecord struct {
	ID          core.ID
	Fingerprint string
}

func newID() core.ID {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return core.ID(hex.EncodeToString(b[:]))
}
func emit(ctx context.Context, store persistence.Store, n Notice) error {
	return realtime.NewStore(store).Append(ctx, realtime.Visible{ID: newID(), OwnerID: n.OwnerID, Topic: "notification.changed", Object: core.Object{Kind: "notification", ID: n.ID}, Version: n.Version, CreatedAt: n.UpdatedAt})
}

// Sync joins the source's business transaction. Repeating a condition never rearms it.
// Recovery resolves the current occurrence; a changed reason or later recurrence gets a new ID.
func Sync(ctx context.Context, store persistence.Store, owner core.ID, in Input, active bool) error {
	if owner.Validate() != nil || in.Source.ID.Validate() != nil || in.Source.Kind == "" || in.Kind == "" {
		return core.Invalid("notification", "source required")
	}
	sum := sha256.Sum256([]byte(in.Kind + "/" + in.Source.Kind + "/" + string(in.Source.ID)))
	key := persistence.Key(owner, core.ID(hex.EncodeToString(sum[:])))
	fingerprint := in.Code + "/" + in.Episode
	return store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var src sourceRecord
		err := tx.Get(ctx, sources, key, &src)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		now := time.Now().UTC()
		if err == nil {
			var old Notice
			if err = tx.Get(ctx, bucket, persistence.Key(owner, src.ID), &old); err != nil {
				return err
			}
			if active && old.ResolvedAt == nil && src.Fingerprint == fingerprint {
				return nil
			}
			if old.ResolvedAt == nil {
				old.ResolvedAt = &now
				old.UpdatedAt = now
				old.Version++
				if err = tx.Put(ctx, bucket, persistence.Key(owner, old.ID), old); err != nil {
					return err
				}
				if err = emit(ctx, store, old); err != nil {
					return err
				}
			}
		}
		if !active {
			return nil
		}
		severity := in.Severity
		if severity == "" {
			severity = "warning"
		}
		n := Notice{ID: newID(), OwnerID: owner, Kind: in.Kind, Code: in.Code, Source: in.Source, AssistantID: in.AssistantID, Label: in.Label, Severity: severity, CreatedAt: now, UpdatedAt: now, Version: 1}
		if err = tx.Put(ctx, bucket, persistence.Key(owner, n.ID), n); err != nil {
			return err
		}
		if err = tx.Put(ctx, sources, key, sourceRecord{n.ID, fingerprint}); err != nil {
			return err
		}
		return emit(ctx, store, n)
	})
}

type Service struct {
	store      persistence.Store
	authorizer policy.Authorizer
}

func New(store persistence.Store, authorizer policy.Authorizer) *Service {
	return &Service{store, authorizer}
}
func (s *Service) authorize(ctx context.Context, p core.Principal, action policy.Action) error {
	if err := p.RequireOwner(); err != nil {
		return err
	}
	if s.authorizer == nil {
		return core.NewError(core.Forbidden, "authorization unavailable")
	}
	return s.authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "realtime", ID: p.OwnerID}, Action: action})
}

type Query struct {
	Filter      string
	Kind        string
	AssistantID core.ID
	Page        core.PageRequest
}
type Page struct {
	Items        []Notice
	NextCursor   string
	UnreadCount  int
	PendingCount int
}
type cursor struct {
	Owner       core.ID
	Filter      string
	Kind        string
	AssistantID core.ID
	At          time.Time
	ID          core.ID
}

func (s *Service) List(ctx context.Context, p core.Principal, q Query) (Page, error) {
	out := Page{Items: []Notice{}}
	if err := s.authorize(ctx, p, policy.Read); err != nil {
		return out, err
	}
	page, err := q.Page.Normalize()
	if err != nil {
		return out, err
	}
	if q.Filter != "" && q.Filter != "all" && q.Filter != "unread" && q.Filter != "pending" && q.Filter != "active" {
		return out, core.Invalid("filter", "invalid notification filter")
	}
	c := cursor{Owner: p.OwnerID, Filter: q.Filter, Kind: q.Kind, AssistantID: q.AssistantID}
	if page.Cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(page.Cursor)
		var previous cursor
		if e != nil || len(raw) > 2048 || json.Unmarshal(raw, &previous) != nil || previous.Owner != c.Owner || previous.Filter != c.Filter || previous.Kind != c.Kind || previous.AssistantID != c.AssistantID {
			return out, core.Invalid("cursor", "query mismatch")
		}
		c = previous
	}
	err = s.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		rows, e := persistence.Select[Notice](ctx, s.store, bucket, persistence.Query{Prefix: string(p.OwnerID) + "/"})
		if e != nil {
			return e
		}
		for _, n := range rows {
			active := n.ResolvedAt == nil && n.DismissedAt == nil
			if active {
				out.PendingCount++
				if n.ReadAt == nil {
					out.UnreadCount++
				}
			}
			if q.Kind != "" && n.Kind != q.Kind || q.AssistantID != "" && n.AssistantID != q.AssistantID {
				continue
			}
			if (q.Filter == "active" || q.Filter == "pending") && !active || q.Filter == "unread" && (!active || n.ReadAt != nil) {
				continue
			}
			if !c.At.IsZero() && (n.CreatedAt.After(c.At) || n.CreatedAt.Equal(c.At) && n.ID >= c.ID) {
				continue
			}
			out.Items = append(out.Items, n)
		}
		sort.Slice(out.Items, func(i, j int) bool {
			a, b := out.Items[i], out.Items[j]
			if !a.CreatedAt.Equal(b.CreatedAt) {
				return a.CreatedAt.After(b.CreatedAt)
			}
			return a.ID > b.ID
		})
		if len(out.Items) > page.Limit {
			out.Items = out.Items[:page.Limit]
			last := out.Items[len(out.Items)-1]
			c.At = last.CreatedAt
			c.ID = last.ID
			raw, _ := json.Marshal(c)
			out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
		}
		return nil
	})
	return out, err
}

// Update is naturally idempotent. A stale device only acknowledges the exact occurrence IDs it saw.
func (s *Service) Update(ctx context.Context, p core.Principal, ids []core.ID, action string) error {
	if err := s.authorize(ctx, p, policy.Write); err != nil {
		return err
	}
	if len(ids) == 0 || len(ids) > 100 || (action != "read" && action != "dismiss") {
		return core.Invalid("notification", "invalid update")
	}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		for _, id := range ids {
			if err := id.Validate(); err != nil {
				return err
			}
			var n Notice
			if err := tx.Get(ctx, bucket, persistence.Key(p.OwnerID, id), &n); err != nil {
				return err
			}
			now := time.Now().UTC()
			changed := false
			if n.ReadAt == nil {
				n.ReadAt = &now
				changed = true
			}
			if action == "dismiss" && n.DismissedAt == nil {
				n.DismissedAt = &now
				changed = true
			}
			if !changed {
				continue
			}
			n.UpdatedAt = now
			n.Version++
			if err := tx.Put(ctx, bucket, persistence.Key(p.OwnerID, id), n); err != nil {
				return err
			}
			if err := emit(ctx, s.store, n); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) Get(ctx context.Context, p core.Principal, id core.ID) (Notice, error) {
	if err := s.authorize(ctx, p, policy.Read); err != nil {
		return Notice{}, err
	}
	if err := id.Validate(); err != nil {
		return Notice{}, err
	}
	return persistence.Load[Notice](ctx, s.store, bucket, persistence.Key(p.OwnerID, id))
}

// Unresolved supplies current source conditions to trusted background reconcilers.
// Dismissed notices remain included until the source condition clears.
func Unresolved(ctx context.Context, store persistence.Store, kind string) ([]Notice, error) {
	return persistence.Select[Notice](ctx, store, bucket, persistence.Query{
		Where: []persistence.Filter{persistence.Eq("Kind", kind), persistence.Eq("ResolvedAt", nil)}, Unordered: true,
	})
}
