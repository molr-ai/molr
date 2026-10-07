package audit

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"sort"
	"strconv"
	"strings"
	"time"
)

type repository struct{ store persistence.Store }

func NewRepository(store persistence.Store) Repository { return &repository{store} }
func (r *repository) Append(ctx context.Context, v Record) error {
	if v.ID.Validate() != nil || v.OwnerID.Validate() != nil || v.At.IsZero() || v.Action == "" {
		return core.Invalid("audit", "id, owner, action and timestamp required")
	}
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var old Record
		key := persistence.Key(v.OwnerID, v.ID)
		e := tx.Get(ctx, "security.audit", key, &old)
		if e == nil {
			return persistence.Conflict("audit event already exists")
		}
		if !persistence.Missing(e) {
			return e
		}
		return tx.Put(ctx, "security.audit", key, v)
	})
}
func (r *repository) List(ctx context.Context, owner core.ID, q Query) (core.Page[Record], error) {
	all, e := persistence.Scan[Record](ctx, r.store, "security.audit")
	if e != nil {
		return core.Page[Record]{}, e
	}
	items := []Record{}
	for _, v := range all {
		if v.OwnerID != owner || (q.Action != "" && v.Action != q.Action) || (q.Outcome != "" && v.Outcome != q.Outcome) || (!q.From.IsZero() && v.At.Before(q.From)) || (!q.Until.IsZero() && !v.At.Before(q.Until)) {
			continue
		}
		items = append(items, v)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].At.Equal(items[j].At) {
			return items[i].ID < items[j].ID
		}
		return items[i].At.After(items[j].At)
	})
	binding := fmt.Sprintf("%s/%s/%s/%s/%s", owner, q.Action, q.Outcome, q.From.UTC(), q.Until.UTC())
	return pageSlice(items, q.Page, binding)
}
func (r *repository) Prune(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 || limit > 10000 {
		return 0, core.Invalid("limit", "1..10000 required")
	}
	var n int64
	e := r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		all, e := persistence.Scan[Record](ctx, tx.Store(), "security.audit")
		if e != nil {
			return e
		}
		sort.Slice(all, func(i, j int) bool { return all[i].At.Before(all[j].At) })
		for _, v := range all {
			if n >= int64(limit) {
				break
			}
			if v.At.Before(before) {
				if e = tx.Delete(ctx, "security.audit", persistence.Key(v.OwnerID, v.ID)); e != nil {
					return e
				}
				n++
			}
		}
		return nil
	})
	return n, e
}

type Options struct {
	ValidatePrincipal func(context.Context, core.Principal) error
	CheckScope        func(context.Context, core.Principal, string) error
}
type Manager struct {
	store   persistence.Store
	repo    Repository
	options Options
}

func NewService(store persistence.Store, o Options) (*Manager, error) {
	if store == nil || o.ValidatePrincipal == nil {
		return nil, core.Invalid("audit", "store and live identity validator required")
	}
	return &Manager{store, NewRepository(store), o}, nil
}
func (m *Manager) Record(ctx context.Context, v Record) error { return m.repo.Append(ctx, v) }
func (m *Manager) List(ctx context.Context, p core.Principal, q Query) (core.Page[Record], error) {
	var out core.Page[Record]
	err := m.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if e := p.RequireOwner(); e != nil {
			return e
		}
		if e := m.options.ValidatePrincipal(ctx, p); e != nil {
			return e
		}
		if p.TokenID != "" {
			if m.options.CheckScope == nil {
				return core.NewError(core.Forbidden, "audit scope unavailable")
			}
			if e := m.options.CheckScope(ctx, p, "audit.read"); e != nil {
				return e
			}
		}
		var err error
		out, err = m.repo.List(ctx, p.OwnerID, q)
		return err
	})
	return out, err
}

var _ Service = (*Manager)(nil)

func pageSlice[T any](items []T, p core.PageRequest, binding string) (core.Page[T], error) {
	p, e := p.Normalize()
	if e != nil {
		return core.Page[T]{}, e
	}
	h := sha256.Sum256([]byte(binding))
	tag := hex.EncodeToString(h[:])
	start := 0
	if p.Cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(p.Cursor)
		if e != nil {
			return core.Page[T]{}, core.Invalid("cursor", "invalid")
		}
		parts := strings.Split(string(b), ":")
		if len(parts) != 2 || parts[0] != tag {
			return core.Page[T]{}, core.Invalid("cursor", "wrong query")
		}
		start, e = strconv.Atoi(parts[1])
		if e != nil || start < 0 || start > len(items) {
			return core.Page[T]{}, core.Invalid("cursor", "invalid position")
		}
	}
	end := start + p.Limit
	if end > len(items) {
		end = len(items)
	}
	out := core.Page[T]{Items: items[start:end]}
	if end < len(items) {
		out.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%d", tag, end)))
	}
	return out, nil
}
