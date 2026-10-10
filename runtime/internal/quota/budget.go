package quota

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"math"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/pricing"
	"sort"
	"time"
)

type Budgets struct {
	Store    persistence.Store
	Now      func() time.Time
	Exchange func() pricing.ExchangeSnapshot
}

func (r *Budgets) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

type ModelRecord struct {
	ModelID                           core.ID
	Reservation                       Reservation
	OwnerID, AssistantID, ExecutionID core.ID
	ReceiptID                         core.ID
	CreatedAt                         time.Time
}
type budgetSpec struct {
	Limit          core.Money
	Start, End, At time.Time
}

func (r *Budgets) budget(ctx context.Context, owner, id core.ID) (budgetSpec, error) {
	a, err := persistence.Load[struct {
		OwnerID core.ID
		Budget  core.AssistantBudget
	}](ctx, r.Store, "business.assistants", persistence.Key(owner, id))
	if err != nil {
		return budgetSpec{}, err
	}
	if a.OwnerID != owner {
		return budgetSpec{}, core.NewError(core.Forbidden, "assistant unavailable")
	}
	if a.Budget.Validate() != nil {
		return budgetSpec{}, core.WithReason(core.NewError(core.BudgetExceeded, "assistant budget not configured"), core.AssistantBudgetMissing)
	}
	settings, err := persistence.Load[struct{ Timezone string }](ctx, r.Store, "business.settings", string(owner))
	if err != nil && !persistence.Missing(err) {
		return budgetSpec{}, err
	}
	zone := settings.Timezone
	if zone == "" {
		zone = "UTC"
	}
	location, err := periodTimezones.load(zone)
	if err != nil {
		return budgetSpec{}, core.Invalid("timezone", "invalid budget timezone")
	}
	now := r.now().In(location)
	start, end := periodInLocation(a.Budget.Period, location, now)
	return budgetSpec{Limit: a.Budget.Limit, Start: start, End: end, At: now}, nil
}
func countsInPeriod(v ModelRecord, start, end time.Time) bool {
	// Only in-flight calls retain their hold across calendar resets.
	return v.Reservation.State == "reserved" || (!v.CreatedAt.Before(start) && v.CreatedAt.Before(end))
}
func (r *Budgets) Reserve(ctx context.Context, request ReserveRequest) (Reservation, error) {
	return r.ReserveObserved(ctx, request, nil)
}
func (r *Budgets) ReserveObserved(ctx context.Context, request ReserveRequest, observe func(any)) (Reservation, error) {
	if e := request.Execution.Validate(); e != nil {
		return Reservation{}, e
	}
	if e := request.Estimate.Validate(); e != nil {
		return Reservation{}, e
	}
	var out Reservation
	err := r.Store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		e := request.Execution
		all, err := persistence.Select[ModelRecord](ctx, tx.Store(), "security.reservations", persistence.Query{Unordered: true, Where: []persistence.Filter{persistence.Eq("OwnerID", e.Principal.OwnerID), persistence.Eq("Reservation.OperationID", e.OperationID)}})
		if err != nil {
			return err
		}
		for _, old := range all {
			if old.OwnerID == e.Principal.OwnerID && old.Reservation.OperationID == e.OperationID {
				if old.AssistantID != e.AssistantID || old.ExecutionID != e.Principal.ExecutionID || old.Reservation.TaskID != e.TaskID || old.Reservation.Reserved != request.Estimate {
					return persistence.Conflict("reservation input changed")
				}
				out = old.Reservation
				return nil
			}
		}
		spec, err := r.budget(ctx, e.Principal.OwnerID, e.AssistantID)
		limit := spec.Limit
		if err != nil {
			return err
		}
		if request.Estimate.Currency != "USD" && request.Estimate.Currency != "CNY" {
			return core.Invalid("currency", "unsupported model currency")
		}
		rates := r.rates()
		estimated, err := convertNative(rates, request.Estimate, limit.Currency)
		if err != nil {
			return err
		}
		all, err = ModelRecords(ctx, tx.Store(), e.Principal.OwnerID, e.AssistantID, "", spec.Start, spec.End)
		if err != nil {
			return err
		}
		var used int64
		for _, old := range all {
			if old.OwnerID != e.Principal.OwnerID || old.AssistantID != e.AssistantID || !countsInPeriod(old, spec.Start, spec.End) {
				continue
			}
			var native *core.Money
			if old.Reservation.State == "reserved" {
				native = &old.Reservation.Reserved
			}
			if old.Reservation.Settled != nil {
				native = old.Reservation.Settled
			}
			if native == nil {
				continue
			}
			amount, err := convertNative(rates, *native, limit.Currency)
			if err != nil {
				return err
			}
			if amount.Micros > math.MaxInt64-used {
				return core.NewError(core.BudgetExceeded, "budget exhausted")
			}
			used += amount.Micros
		}
		if observe != nil {
			observe(map[string]any{"Limit": limit, "Used": core.Money{Currency: limit.Currency, Micros: used}, "Required": estimated, "PeriodStart": spec.Start, "PeriodEnd": spec.End, "Exchange": rates})
		}
		if estimated.Micros > limit.Micros || used > limit.Micros-estimated.Micros {
			return core.NewError(core.BudgetExceeded, "budget exhausted")
		}
		out = Reservation{BudgetEstimate: &estimated, Exchange: rates, ID: newID(), OperationID: e.OperationID, TaskID: e.TaskID, Reserved: request.Estimate, State: "reserved"}
		return tx.Put(ctx, "security.reservations", persistence.Key(e.Principal.OwnerID, out.ID), ModelRecord{ModelID: request.ModelID, Reservation: out, OwnerID: e.Principal.OwnerID, AssistantID: e.AssistantID, ExecutionID: e.Principal.ExecutionID, CreatedAt: spec.At})
	})
	return out, err
}
func (r *Budgets) Settle(ctx context.Context, owner core.ID, request SettleRequest) (Reservation, error) {
	if e := request.Actual.Validate(); e != nil {
		return Reservation{}, e
	}
	if request.ReceiptID.Validate() != nil {
		return Reservation{}, core.Invalid("receipt_id", "required")
	}
	var out Reservation
	err := r.Store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var record ModelRecord
		key := persistence.Key(owner, request.ReservationID)
		if e := tx.Get(ctx, "security.reservations", key, &record); e != nil {
			return e
		}
		if record.Reservation.OperationID != request.OperationID {
			return core.NewError(core.Forbidden, "reservation operation mismatch")
		}
		if record.Reservation.Reserved.Currency != request.Actual.Currency {
			return core.Invalid("currency", "settlement currency mismatch")
		}
		if record.Reservation.Settled != nil {
			if *record.Reservation.Settled != request.Actual || record.ReceiptID != request.ReceiptID {
				return persistence.Conflict("conflicting settlement")
			}
			out = record.Reservation
			return nil
		}
		if record.Reservation.State == "released" {
			return core.NewError(core.FailedPrecondition, "reservation released")
		}
		record.Reservation.Settled = &request.Actual
		record.Reservation.State = "settled"
		record.ReceiptID = request.ReceiptID
		out = record.Reservation
		return tx.Put(ctx, "security.reservations", key, record)
	})
	return out, err
}
func (r *Budgets) SetReservationState(ctx context.Context, owner, id core.ID, state string) error {
	return r.Store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var record ModelRecord
		key := persistence.Key(owner, id)
		if e := tx.Get(ctx, "security.reservations", key, &record); e != nil {
			return e
		}
		if state != "unknown" && state != "released" {
			return core.Invalid("state", "unsupported reservation transition")
		}
		if record.Reservation.State == "settled" || record.Reservation.State == "released" {
			if record.Reservation.State == state {
				return nil
			}
			return core.NewError(core.FailedPrecondition, "reservation is terminal")
		}
		record.Reservation.State = state
		return tx.Put(ctx, "security.reservations", key, record)
	})
}
func (r *Budgets) rates() *pricing.ExchangeSnapshot {
	if r.Exchange == nil {
		return nil
	}
	s := r.Exchange()
	return &s
}
func convertNative(rates *pricing.ExchangeSnapshot, amount core.Money, target string) (core.Money, error) {
	if amount.Currency == target {
		return amount, amount.Validate()
	}
	if rates == nil {
		return core.Money{}, core.NewError(core.Unavailable, "exchange rates unavailable")
	}
	return rates.Convert(amount, target, true)
}
func (r *Budgets) Usage(ctx context.Context, owner core.ID, q UsageQuery) (Usage, error) {
	preference, err := persistence.Load[struct{ Currency string }](ctx, r.Store, "business.settings", string(owner))
	if err != nil && !persistence.Missing(err) {
		return Usage{}, err
	}
	currency := preference.Currency
	if currency == "" {
		currency = "USD"
	}
	out := Usage{Spent: core.Money{Currency: currency}, Reserved: core.Money{Currency: currency}, Exchange: r.rates()}
	var period *budgetSpec
	if q.CurrentPeriod {
		if q.AssistantID == "" || !q.From.IsZero() || !q.Until.IsZero() {
			return Usage{}, core.Invalid("usage", "assistant required; current period cannot combine date filters")
		}
		spec, err := r.budget(ctx, owner, q.AssistantID)
		if err != nil {
			return Usage{}, err
		}
		period = &spec
		out.Limit = &spec.Limit
		out.PeriodStart = &spec.Start
		out.PeriodEnd = &spec.End
	}
	from, until := q.From, q.Until
	if period != nil {
		from, until = period.Start, period.End
	}
	filters := []persistence.Filter{persistence.Eq("OwnerID", owner)}
	if q.AssistantID != "" {
		filters = append(filters, persistence.Eq("AssistantID", q.AssistantID))
	}
	if q.TaskID != "" {
		filters = append(filters, persistence.Eq("Reservation.TaskID", q.TaskID))
	}
	query := periodQuery(filters, from, until, "Reservation.State", []string{"reserved"})
	if period == nil {
		query.Any = nil
		query.Where = append(query.Where, timeFilters(from, until)...)
	}
	all, err := persistence.Select[ModelRecord](ctx, r.Store, "security.reservations", query)
	if err != nil {
		return Usage{}, err
	}
	spent, reserved := map[string]int64{}, map[string]int64{}
	add := func(totals map[string]int64, amount core.Money) error {
		if err := amount.Validate(); err != nil {
			return err
		}
		if amount.Micros > math.MaxInt64-totals[amount.Currency] {
			return core.NewError(core.Internal, "usage overflow")
		}
		totals[amount.Currency] += amount.Micros
		return nil
	}
	for _, v := range all {
		if v.OwnerID != owner || (q.AssistantID != "" && q.AssistantID != v.AssistantID) || (q.TaskID != "" && q.TaskID != v.Reservation.TaskID) || (!q.From.IsZero() && v.CreatedAt.Before(q.From)) || (!q.Until.IsZero() && !v.CreatedAt.Before(q.Until)) || (period != nil && !countsInPeriod(v, period.Start, period.End)) {
			continue
		}
		if v.Reservation.Settled != nil {
			if err := add(spent, *v.Reservation.Settled); err != nil {
				return Usage{}, err
			}
		}
		if v.Reservation.State == "reserved" {
			if err := add(reserved, v.Reservation.Reserved); err != nil {
				return Usage{}, err
			}
		}
		if v.Reservation.State == "unknown" {
			out.UnknownOperations = append(out.UnknownOperations, v.Reservation.OperationID)
		}
	}
	summarize := func(totals map[string]int64, total *core.Money, original *[]core.Money) error {
		keys := []string{}
		for k := range totals {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			native := core.Money{Currency: key, Micros: totals[key]}
			*original = append(*original, native)
			converted, err := convertNative(out.Exchange, native, currency)
			if err != nil {
				return err
			}
			if converted.Micros > math.MaxInt64-total.Micros {
				return core.NewError(core.Internal, "usage overflow")
			}
			total.Micros += converted.Micros
		}
		return nil
	}
	if err := summarize(spent, &out.Spent, &out.OriginalSpent); err != nil {
		return Usage{}, err
	}
	if err := summarize(reserved, &out.Reserved, &out.OriginalReserved); err != nil {
		return Usage{}, err
	}
	return out, nil
}
func newID() core.ID {
	var b [24]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic("operating system entropy unavailable")
	}
	return core.ID(base64.RawURLEncoding.EncodeToString(b[:]))
}
