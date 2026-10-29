package policy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/pricing"
	"molr.ai/runtime/internal/quota"
	"time"
)

type reservationRecord = quota.ModelRecord
type repository struct {
	store    persistence.Store
	now      func() time.Time
	exchange func() pricing.ExchangeSnapshot
}

func NewRepository(store persistence.Store) Repository {
	return &repository{store: store, now: time.Now}
}
func (r *repository) Grants(ctx context.Context, owner core.ID, q Query) (core.Page[Grant], error) {
	filters := []persistence.Filter{persistence.Eq("OwnerID", owner)}
	if q.AssistantID != "" {
		filters = append(filters, persistence.Eq("AssistantID", q.AssistantID))
	}
	if q.ResourceID != "" {
		filters = append(filters, persistence.Eq("ResourceID", q.ResourceID))
	}
	request := q.Page
	q.Page = core.PageRequest{}
	return persistence.Page[Grant](ctx, r.store, "security.grants", persistence.Query{Where: filters}, request, q, nil)
}
func (r *repository) InsertGrant(ctx context.Context, g Grant) error {
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		key := persistence.Key(g.OwnerID, g.ID)
		var old Grant
		e := tx.Get(ctx, "security.grants", key, &old)
		if e == nil {
			return persistence.Conflict("grant exists")
		}
		if !persistence.Missing(e) {
			return e
		}
		g.Version = 1
		return tx.Put(ctx, "security.grants", key, g)
	})
}
func (r *repository) RevokeGrant(ctx context.Context, owner, id core.ID, version core.Version, at time.Time) error {
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		key := persistence.Key(owner, id)
		var g Grant
		if e := tx.Get(ctx, "security.grants", key, &g); e != nil {
			return e
		}
		if version != g.Version {
			return persistence.Conflict("grant version mismatch")
		}
		g.Version++
		g.RevokedAt = &at
		g.UpdatedAt = at.UTC()
		return tx.Put(ctx, "security.grants", key, g)
	})
}

func (r *repository) budgets() *quota.Budgets {
	return &quota.Budgets{Store: r.store, Now: r.now, Exchange: r.exchange}
}
func (r *repository) Reserve(ctx context.Context, q ReserveRequest) (Reservation, error) {
	return r.budgets().Reserve(ctx, q)
}
func (r *repository) reserve(ctx context.Context, q ReserveRequest, observe func(any)) (Reservation, error) {
	return r.budgets().ReserveObserved(ctx, q, observe)
}
func (r *repository) Settle(ctx context.Context, owner core.ID, q SettleRequest) (Reservation, error) {
	return r.budgets().Settle(ctx, owner, q)
}
func (r *repository) SetReservationState(ctx context.Context, owner, id core.ID, state string) error {
	return r.budgets().SetReservationState(ctx, owner, id, state)
}
func (r *repository) Usage(ctx context.Context, owner core.ID, q UsageQuery) (Usage, error) {
	return r.budgets().Usage(ctx, owner, q)
}
func newID() core.ID {
	var b [24]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic("operating system entropy unavailable")
	}
	return core.ID(base64.RawURLEncoding.EncodeToString(b[:]))
}

type mutationReceipt struct {
	Digest string
	Result json.RawMessage
}

func payloadDigest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
