package app

import (
	"context"
	"encoding/json"
	"fmt"
	"molr.ai/runtime/internal/accountview"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/quota"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func putAnalyticsCall(t *testing.T, f multiUserFixture, owner core.ID, id, assistant, state, at, currency string, micros int64, input, output, cached int64) {
	t.Helper()
	stamp, err := time.Parse(time.RFC3339Nano, at)
	need(t, err)
	r := quota.ModelRecord{OwnerID: owner, AssistantID: core.ID(assistant), ModelID: "analytics-model", CreatedAt: stamp, Reservation: quota.Reservation{ID: core.ID(id), OperationID: core.ID("op-" + id), State: state, Reserved: core.Money{Currency: currency, Micros: 100}}}
	if state == "settled" {
		r.Reservation.Settled = &core.Money{Currency: currency, Micros: micros}
		r.ReceiptID = core.ID("receipt-" + id)
	}
	need(t, f.b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if r.ReceiptID != "" {
			if err := tx.Put(ctx, "agent.model_receipts", persistence.Key(owner, r.ReceiptID), model.Receipt{ID: r.ReceiptID, OperationID: r.Reservation.OperationID, ModelID: r.ModelID, ModelName: "Shared model", OutcomeKnown: true, Usage: model.Usage{InputTokens: input, OutputTokens: output, CachedTokens: cached}, CreatedAt: stamp.Add(time.Hour)}); err != nil {
				return err
			}
		}
		return tx.Put(ctx, "security.reservations", persistence.Key(owner, r.Reservation.ID), r)
	}))
}
func TestUsageAnalyticsCalendarScopeAndEvidence(t *testing.T) {
	f := newMultiUserFixture(t)
	ctx := t.Context()
	m := f.b.Services.Auth.(*auth.Manager)
	putAnalyticsCall(t, f, f.alice.OwnerID, "a", "private-assistant", "settled", "2026-10-01T15:59:59Z", "USD", 12345, 100, 20, 60)
	putAnalyticsCall(t, f, f.alice.OwnerID, "b", "dismissed", "settled", "2026-10-01T16:00:00Z", "CNY", 42, 200, 30, 0)
	putAnalyticsCall(t, f, f.alice.OwnerID, "free", "dismissed", "settled", "2026-10-02T02:00:00Z", "USD", 0, 10, 5, 0)
	putAnalyticsCall(t, f, f.alice.OwnerID, "unknown", "dismissed", "unknown", "2026-10-02T03:00:00Z", "USD", 0, 0, 0, 0)
	putAnalyticsCall(t, f, f.alice.OwnerID, "active", "dismissed", "reserved", "2026-10-02T04:00:00Z", "USD", 0, 0, 0, 0)
	putAnalyticsCall(t, f, f.alice.OwnerID, "old-active", "dismissed", "reserved", "2026-09-01T04:00:00Z", "USD", 0, 0, 0, 0)
	putAnalyticsCall(t, f, f.alice.OwnerID, "released", "dismissed", "released", "2026-10-02T04:00:00Z", "USD", 0, 0, 0, 0)
	putAnalyticsCall(t, f, f.bob.OwnerID, "other", "private-bob", "settled", "2026-10-02T04:00:00Z", "USD", 400, 900, 100, 0)
	need(t, f.b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.assistants", persistence.Key(f.alice.OwnerID, "dismissed"), map[string]any{"OwnerID": f.alice.OwnerID, "ID": "dismissed", "Name": "Private assistant title", "State": "dismissed"})
	}))
	q := accountview.UsageQuery{From: "2026-10-01", Until: "2026-10-03", Timezone: "Asia/Shanghai", GroupBy: "assistant", Page: 1}
	run := func(q accountview.UsageQuery, admin bool) accountview.AnalyticsView {
		t.Helper()
		p := f.alice
		if admin {
			p = f.admin
		}
		v, e := usageAnalytics(ctx, f.b.store, m, p, q, admin, "USD", f.b.Exchange.Snapshot())
		need(t, e)
		return v
	}
	own := run(q, false)
	if own.Summary.TotalTokens != 365 || own.Summary.InputTokens != 310 || own.Summary.CachedTokens != 60 || own.Summary.ModelCalls != 5 || own.Summary.MissingTokenCalls != 1 || own.Summary.UnknownCostCalls != 1 || own.Summary.ActiveCalls != 1 {
		t.Fatalf("incorrect total: %+v", own.Summary)
	}
	if len(own.Daily) != 2 || own.Daily[0].TotalTokens != 120 || own.Daily[1].TotalTokens != 245 || own.ActiveDays != 2 || own.ActiveUsers != 1 {
		t.Fatal(own.Daily)
	}
	if len(own.Summary.Spent) != 2 || own.Summary.Spent[0].Micros != 42 || own.Summary.Spent[1].Micros != 12345 {
		t.Fatal(own.Summary.Spent)
	}
	if own.Rows[0].State != "dismissed" {
		t.Fatal(own.Rows)
	}
	q.Day = "2026-10-01"
	single := run(q, false)
	if single.Summary.TotalTokens != 365 || len(single.Rows) != 1 || single.Rows[0].TotalTokens != 120 {
		t.Fatal(single)
	}
	q.Day = ""
	q.AssistantID = "dismissed"
	filtered := run(q, false)
	if filtered.Summary.TotalTokens != 245 {
		t.Fatal(filtered.Summary)
	}
	q.AssistantID = ""
	q.GroupBy = "user"
	all := run(q, true)
	if all.Summary.TotalTokens != 1365 || all.ActiveUsers != 2 {
		t.Fatal(all.Summary)
	}
	raw, err := json.Marshal(all)
	need(t, err)
	for _, private := range []string{"private-assistant", "private-bob", "Private assistant title", "receipt-a", "op-a"} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("private field exposed: %s", private)
		}
	}
	q.UserID = f.alice.OwnerID
	member := run(q, true)
	if member.Summary.TotalTokens != own.Summary.TotalTokens || member.Summary.ModelCalls != own.Summary.ModelCalls {
		t.Fatal(member.Summary)
	}
	q.UserID = ""
	q.GroupBy = "group"
	q.GroupID = f.group.ID
	if got := run(q, true); got.TotalRows != 1 || got.Rows[0].TotalTokens != 1365 {
		t.Fatal(got.Rows)
	}
	if _, err := usageAnalytics(ctx, f.b.store, m, f.alice, q, true, "USD", f.b.Exchange.Snapshot()); core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
	}
}
func TestUsageAnalyticsValidationDSTAndPagination(t *testing.T) {
	f := newMultiUserFixture(t)
	m := f.b.Services.Auth.(*auth.Manager)
	ctx := t.Context()
	base := accountview.UsageQuery{From: "2026-03-08", Until: "2026-03-09", Timezone: "America/New_York", GroupBy: "assistant", Page: 1}
	putAnalyticsCall(t, f, f.alice.OwnerID, "before", "before", "settled", "2026-03-08T04:59:59.999999999Z", "USD", 1, 1, 0, 0)
	putAnalyticsCall(t, f, f.alice.OwnerID, "start", "start", "settled", "2026-03-08T05:00:00Z", "USD", 2, 2, 0, 0)
	putAnalyticsCall(t, f, f.alice.OwnerID, "end", "end", "settled", "2026-03-09T03:59:59.999999999Z", "USD", 3, 3, 0, 0)
	putAnalyticsCall(t, f, f.alice.OwnerID, "after", "after", "settled", "2026-03-09T04:00:00Z", "USD", 4, 4, 0, 0)
	v, err := usageAnalytics(ctx, f.b.store, m, f.alice, base, false, "USD", f.b.Exchange.Snapshot())
	need(t, err)
	if v.Summary.TotalTokens != 5 || len(v.Daily) != 1 {
		t.Fatal(v)
	}
	for _, change := range []func(*accountview.UsageQuery){func(q *accountview.UsageQuery) { q.Timezone = "invalid" }, func(q *accountview.UsageQuery) { q.Until = "2028-01-01" }, func(q *accountview.UsageQuery) { q.From = "2026-02-30" }, func(q *accountview.UsageQuery) { q.Day = "2026-03-09" }, func(q *accountview.UsageQuery) { q.GroupBy = "user" }, func(q *accountview.UsageQuery) { q.UserID = f.bob.OwnerID }, func(q *accountview.UsageQuery) { q.Sort = "bad" }} {
		q := base
		change(&q)
		if _, e := usageAnalytics(ctx, f.b.store, m, f.alice, q, false, "USD", f.b.Exchange.Snapshot()); e == nil {
			t.Fatal("accepted invalid query", q)
		}
	}
	for i := 0; i < 23; i++ {
		id := fmt.Sprintf("page-%02d", i)
		putAnalyticsCall(t, f, f.alice.OwnerID, id, id, "settled", "2026-03-08T12:00:00Z", "USD", int64(i+10), int64(i+10), 0, 0)
	}
	base.Sort = "spend"
	base.Page = 2
	v, err = usageAnalytics(ctx, f.b.store, m, f.alice, base, false, "CNY", f.b.Exchange.Snapshot())
	need(t, err)
	if len(v.Rows) != 5 || v.TotalRows != 25 || v.Rows[0].TotalTokens != 12 || v.Rows[4].TotalTokens != 2 {
		t.Fatal(v.Rows)
	}
}
func TestUsageAnalyticsHTTPAuthorization(t *testing.T) {
	f := newMultiUserFixture(t)
	for _, tc := range []struct {
		path   string
		status int
	}{{"/api/account/usage?from=2026-01-01&until=2027-01-01", 200}, {"/api/admin/usage/analytics?from=2026-01-01&until=2027-01-01", 403}, {"/api/account/usage?from=2026-01-01&until=2027-01-01&user_id=other", 400}, {"/api/account/usage?page=-1", 400}} {
		r := httptest.NewRequest("GET", "https://runtime.example"+tc.path, nil)
		r.RemoteAddr = "127.0.0.1:10000"
		r.Header.Set("X-Forwarded-Proto", "https")
		r.AddCookie(&http.Cookie{Name: "__Host-molr_session", Value: f.aliceSession.Token.Reveal()})
		w := httptest.NewRecorder()
		f.b.HTTP.Handler().ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
}

func TestSubscriptionValuationReusesLedgerAndLabelsTotals(t *testing.T) {
	f := newMultiUserFixture(t)
	ctx := t.Context()
	putAnalyticsCall(t, f, f.alice.OwnerID, "reference", "assistant", "settled", "2026-10-02T03:00:00Z", "USD", 42, 10, 2, 0)
	need(t, f.b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		receipt, err := persistence.Load[model.Receipt](ctx, tx.Store(), "agent.model_receipts", persistence.Key(f.alice.OwnerID, "receipt-reference"))
		if err != nil {
			return err
		}
		receipt.PricingInfo = &model.PricingInfo{Basis: "subscription-reference"}
		return tx.Put(ctx, "agent.model_receipts", persistence.Key(f.alice.OwnerID, receipt.ID), receipt)
	}))
	result, err := usageAnalytics(ctx, f.b.store, f.b.Services.Auth.(*auth.Manager), f.alice, accountview.UsageQuery{From: "2026-10-01", Until: "2026-10-03", Timezone: "UTC", GroupBy: "model", Page: 1}, false, "USD", f.b.Exchange.Snapshot())
	need(t, err)
	if result.Summary.ReferenceCalls != 1 || result.Rows[0].ReferenceCalls != 1 || result.Daily[1].ReferenceCalls != 1 || result.Summary.Spent[0].Micros != 42 || result.Summary.TotalTokens != 12 {
		t.Fatalf("reference valuation was lost: %+v", result.Summary)
	}
}

func TestUsageAnalyticsOmitsEmptyModelOptionButPreservesTotals(t *testing.T) {
	f := newMultiUserFixture(t)
	putAnalyticsCall(t, f, f.alice.OwnerID, "known", "assistant", "settled", "2026-10-01T00:00:00Z", "USD", 10, 20, 5, 0)
	putAnalyticsCall(t, f, f.alice.OwnerID, "missing", "assistant", "settled", "2026-10-01T00:00:00Z", "USD", 15, 30, 5, 0)
	need(t, f.b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		key := persistence.Key(f.alice.OwnerID, "missing")
		var r quota.ModelRecord
		if err := tx.Get(ctx, "security.reservations", key, &r); err != nil {
			return err
		}
		r.ModelID = ""
		if err := tx.Put(ctx, "security.reservations", key, r); err != nil {
			return err
		}
		var receipt model.Receipt
		key = persistence.Key(f.alice.OwnerID, r.ReceiptID)
		if err := tx.Get(ctx, "agent.model_receipts", key, &receipt); err != nil {
			return err
		}
		receipt.ModelID = ""
		receipt.ModelName = ""
		return tx.Put(ctx, "agent.model_receipts", key, receipt)
	}))
	q := accountview.UsageQuery{From: "2026-10-01", Until: "2026-10-02", GroupBy: "model"}
	got, err := usageAnalytics(t.Context(), f.b.store, f.b.Services.Auth.(*auth.Manager), f.alice, q, false, "USD", f.b.Exchange.Snapshot())
	need(t, err)
	if len(got.Options.Models) != 1 || got.Options.Models[0].ID != "analytics-model" || got.Summary.ModelCalls != 2 || got.Summary.TotalTokens != 60 || got.Summary.Spent[0].Micros != 25 || len(got.Rows) != 2 {
		t.Fatalf("unknown model changed totals/options: %+v", got)
	}
	q.ModelID = "analytics-model"
	got, err = usageAnalytics(t.Context(), f.b.store, f.b.Services.Auth.(*auth.Manager), f.alice, q, false, "USD", f.b.Exchange.Snapshot())
	need(t, err)
	if got.Summary.ModelCalls != 1 || got.Summary.TotalTokens != 25 {
		t.Fatal(got.Summary)
	}
}

func TestAnalyticsReceiptsBatchesIndexesAndPreservesMissingEvidence(t *testing.T) {
	f := newMultiUserFixture(t)
	records := make([]quota.ModelRecord, 515)
	need(t, f.b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := range records {
			id := core.ID(fmt.Sprintf("batch-%d", i))
			records[i] = quota.ModelRecord{Reservation: quota.Reservation{OperationID: id}}
			if i == 514 {
				continue
			}
			if i%2 == 0 {
				records[i].ReceiptID = id
			} else if err := tx.Put(ctx, "agent.model_receipt_index", persistence.Key(f.alice.OwnerID, id), map[string]any{"ID": id}); err != nil {
				return err
			}
			if err := tx.Put(ctx, "agent.model_receipts", persistence.Key(f.alice.OwnerID, id), map[string]any{"ID": id, "ModelName": "Fixture model", "Usage": model.Usage{InputTokens: int64(i)}, "OutcomeKnown": true, "Unused": strings.Repeat("x", 4096)}); err != nil {
				return err
			}
		}
		return tx.Put(ctx, "agent.model_receipts", persistence.Key(f.bob.OwnerID, "batch-514"), model.Receipt{ID: "batch-514", ModelName: "Foreign model"})
	}))
	need(t, f.b.store.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		got, err := analyticsReceipts(ctx, tx, f.alice.OwnerID, records)
		if err != nil {
			return err
		}
		if len(got) != 514 || records[513].ReceiptID != "batch-513" || got["batch-513"].Usage.InputTokens != 513 || records[514].ReceiptID != "" {
			t.Fatalf("bad evidence batch: %d %+v", len(got), records[513:])
		}
		return nil
	}))
}
