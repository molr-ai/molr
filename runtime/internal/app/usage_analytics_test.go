package app

import (
	"context"
	"encoding/json"
	"fmt"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/api"
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
	q := api.UsageQuery{From: "2026-10-01", Until: "2026-10-03", Timezone: "Asia/Shanghai", GroupBy: "assistant", Page: 1}
	run := func(q api.UsageQuery, admin bool) analyticsView {
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
	base := api.UsageQuery{From: "2026-03-08", Until: "2026-03-09", Timezone: "America/New_York", GroupBy: "assistant", Page: 1}
	putAnalyticsCall(t, f, f.alice.OwnerID, "before", "before", "settled", "2026-03-08T04:59:59.999999999Z", "USD", 1, 1, 0, 0)
	putAnalyticsCall(t, f, f.alice.OwnerID, "start", "start", "settled", "2026-03-08T05:00:00Z", "USD", 2, 2, 0, 0)
	putAnalyticsCall(t, f, f.alice.OwnerID, "end", "end", "settled", "2026-03-09T03:59:59.999999999Z", "USD", 3, 3, 0, 0)
	putAnalyticsCall(t, f, f.alice.OwnerID, "after", "after", "settled", "2026-03-09T04:00:00Z", "USD", 4, 4, 0, 0)
	v, err := usageAnalytics(ctx, f.b.store, m, f.alice, base, false, "USD", f.b.Exchange.Snapshot())
	need(t, err)
	if v.Summary.TotalTokens != 5 || len(v.Daily) != 1 {
		t.Fatal(v)
	}
	for _, change := range []func(*api.UsageQuery){func(q *api.UsageQuery) { q.Timezone = "invalid" }, func(q *api.UsageQuery) { q.Until = "2028-01-01" }, func(q *api.UsageQuery) { q.From = "2026-02-30" }, func(q *api.UsageQuery) { q.Day = "2026-03-09" }, func(q *api.UsageQuery) { q.GroupBy = "user" }, func(q *api.UsageQuery) { q.UserID = f.bob.OwnerID }, func(q *api.UsageQuery) { q.Sort = "bad" }} {
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
