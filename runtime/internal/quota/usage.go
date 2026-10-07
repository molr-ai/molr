package quota

import (
	"context"
	"math"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/pricing"
	"sort"
	"time"
)

// Totals is shared by admission checks and reporting. Carried counts unresolved
// calls from before the selected period; these still consume allowance.
type Totals struct {
	Amounts  []core.Money `json:"-"`
	Calls    int64
	Active   int
	Unknown  int
	Carried  int
	Spent    []core.Money
	Reserved []core.Money
}

func emptyTotals() Totals { return Totals{Spent: []core.Money{}, Reserved: []core.Money{}} }

// SQLite calendar comparisons have millisecond precision. Fetch a narrow margin
// and apply exact Go time boundaries below, preserving nanosecond edge cases.
func timeFilters(from, until time.Time) []persistence.Filter {
	out := []persistence.Filter{}
	if !from.IsZero() {
		out = append(out, persistence.Filter{Field: "CreatedAt", Op: ">=", Value: from.Add(-time.Millisecond).UTC().Format(time.RFC3339Nano)})
	}
	if !until.IsZero() {
		out = append(out, persistence.Filter{Field: "CreatedAt", Op: "<", Value: until.Add(time.Millisecond).UTC().Format(time.RFC3339Nano)})
	}
	return out
}
func periodQuery(filters []persistence.Filter, from, until time.Time, state string, pending []string) persistence.Query {
	q := persistence.Query{Unordered: true, Where: filters}
	dates := timeFilters(from, until)
	if len(dates) == 0 {
		return q
	}
	q.Any = append(q.Any, dates)
	for _, value := range pending {
		q.Any = append(q.Any, []persistence.Filter{persistence.Eq(state, value)})
	}
	return q
}
func InPeriod(at, from, until time.Time) bool {
	return (from.IsZero() || !at.Before(from)) && (until.IsZero() || at.Before(until))
}
func ModelRecords(ctx context.Context, store persistence.Store, owner, assistant, model core.ID, from, until time.Time) ([]ModelRecord, error) {
	filters := []persistence.Filter{persistence.Eq("OwnerID", owner)}
	if assistant != "" {
		filters = append(filters, persistence.Eq("AssistantID", assistant))
	}
	if model != "" {
		filters = append(filters, persistence.Eq("ModelID", model))
	}
	rows, err := persistence.Select[ModelRecord](ctx, store, "security.reservations", periodQuery(filters, from, until, "Reservation.State", []string{"reserved"}))
	if err != nil {
		return nil, err
	}
	out := []ModelRecord{}
	for _, row := range rows {
		if row.Reservation.State == "reserved" || InPeriod(row.CreatedAt, from, until) {
			out = append(out, row)
		}
	}
	return out, nil
}
func ModelByOperation(ctx context.Context, store persistence.Store, owner, operation core.ID) (*ModelRecord, error) {
	rows, err := persistence.Select[ModelRecord](ctx, store, "security.reservations", persistence.Query{Unordered: true, Where: []persistence.Filter{persistence.Eq("OwnerID", owner), persistence.Eq("Reservation.OperationID", operation)}, Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}
func ModelTotals(ctx context.Context, store persistence.Store, owner, model core.ID, from, until time.Time) (Totals, error) {
	out := emptyTotals()
	rows, err := ModelRecords(ctx, store, owner, "", model, from, until)
	if err != nil {
		return out, err
	}
	spent, held := map[string]int64{}, map[string]int64{}
	for _, row := range rows {
		v := row.Reservation
		if v.State == "released" {
			continue
		}
		out.Calls++
		if v.State == "unknown" {
			out.Unknown++
		}
		if v.State == "reserved" {
			out.Active++
			if !InPeriod(row.CreatedAt, from, until) {
				out.Carried++
			}
		}
		if v.State != "reserved" && v.Settled == nil {
			continue // Unknown costs are history, not an active reservation or confirmed spend.
		}
		amount := v.Reserved
		totals := held
		if v.Settled != nil {
			amount = *v.Settled
			totals = spent
		}
		if err := amount.Validate(); err != nil {
			return out, err
		}
		if amount.Micros > math.MaxInt64-totals[amount.Currency] {
			return out, core.NewError(core.Internal, "usage overflow")
		}
		totals[amount.Currency] += amount.Micros
		out.Amounts = append(out.Amounts, amount)
	}
	money := func(values map[string]int64) []core.Money {
		out := []core.Money{}
		for currency, micros := range values {
			out = append(out, core.Money{Currency: currency, Micros: micros})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Currency < out[j].Currency })
		return out
	}
	out.Spent, out.Reserved = money(spent), money(held)
	return out, nil
}

// CallTotals reads only accounting fields, never arguments, outputs or credentials.
// Connection owner may be empty when enforcing the shared connection ceiling.
func CallTotals(ctx context.Context, store persistence.Store, kind string, owner, resource, exclude core.ID, from, until time.Time) (Totals, error) {
	out := emptyTotals()
	bucket, resourceField := "agent.operations", "Action"
	if kind == "connection" {
		bucket, resourceField = "plugins.http-calls", "Connection.ID"
	} else if kind != "tool" {
		return out, core.Invalid("usage.kind", "unsupported call kind")
	}
	filters := []persistence.Filter{}
	if owner != "" {
		filters = append(filters, persistence.Eq("OwnerID", owner))
	}
	if resource != "" {
		filters = append(filters, persistence.Eq(resourceField, resource))
	}
	if exclude != "" {
		filters = append(filters, persistence.Filter{Field: "ID", Op: "!=", Value: exclude})
	}
	pending := []string{"running"}
	if kind == "connection" {
		pending = append(pending, "prepared")
	}
	q := periodQuery(filters, from, until, "State", pending)
	q.Fields = []string{"ID", "Action", "State", "CreatedAt"}
	rows, err := persistence.Select[struct {
		ID            core.ID
		Action, State string
		CreatedAt     time.Time
	}](ctx, store, bucket, q)
	if err != nil {
		return out, err
	}
	for _, row := range rows {
		if kind == "tool" && (row.Action == "model.generate" || row.State == "prepared") {
			continue
		}
		active := row.State == "running" || (kind == "connection" && row.State == "prepared")
		if !active && !InPeriod(row.CreatedAt, from, until) {
			continue
		}
		out.Calls++
		if active {
			out.Active++
			if !InPeriod(row.CreatedAt, from, until) {
				out.Carried++
			}
		}
		if row.State == "unknown" {
			out.Unknown++
		}
	}
	return out, nil
}
func Exceeded() error {
	return core.WithReason(core.NewError(core.BudgetExceeded, "user group quota exceeded"), "group_quota_exceeded")
}
func CheckCalls(used Totals, calls int64, concurrent int) error {
	if calls > 0 && used.Calls >= calls || concurrent > 0 && used.Active >= concurrent {
		return Exceeded()
	}
	return nil
}
func CheckMoney(used Totals, estimate, limit core.Money, rates pricing.ExchangeSnapshot) error {
	var total int64
	for _, a := range used.Amounts {
		v, err := rates.Convert(a, limit.Currency, true)
		if err != nil {
			return err
		}
		if v.Micros > math.MaxInt64-total {
			return Exceeded()
		}
		total += v.Micros
	}
	next, err := rates.Convert(estimate, limit.Currency, true)
	if err != nil {
		return err
	}
	if next.Micros > limit.Micros || total > limit.Micros-next.Micros {
		return Exceeded()
	}
	return nil
}
func AttachmentBytes(ctx context.Context, store persistence.Store, owner core.ID) (int64, error) {
	q := persistence.Query{Unordered: true, Fields: []string{"Size"}, Where: []persistence.Filter{persistence.Eq("State", "ready")}}
	if owner != "" {
		q.Where = append(q.Where, persistence.Eq("OwnerID", owner))
	}
	rows, err := persistence.Select[struct{ Size int64 }](ctx, store, "storage.files", q)
	if err != nil {
		return 0, err
	}
	var size int64
	for _, r := range rows {
		if r.Size < 0 || r.Size > math.MaxInt64-size {
			return 0, core.NewError(core.Internal, "usage overflow")
		}
		size += r.Size
	}
	return size, nil
}
