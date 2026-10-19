package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"molr.ai/runtime/internal/catalog"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type historyObservedStore struct {
	persistence.Store
	bytes int
}
type historyObservedTx struct {
	persistence.Tx
	parent *historyObservedStore
}

func (s *historyObservedStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return fn(ctx, &historyObservedTx{Tx: tx, parent: s})
	})
}
func (t *historyObservedTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	rows, err := t.Tx.Query(ctx, bucket, q)
	if bucket == "plugins.http-calls" {
		for _, row := range rows {
			t.parent.bytes += len(row.Data)
		}
	}
	return rows, err
}
func historyProjectionFixture(tb testing.TB) (*Service, core.Principal, ConnectionRequest, *historyObservedStore) {
	tb.Helper()
	db, err := sqlitestore.Open(filepath.Join(tb.TempDir(), "history.db"))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { db.Close() })
	p := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
	connection := Connection{ID: "connection", OwnerID: catalog.Scope, PluginID: "fixture.history", ProviderID: "provider"}
	payload, _ := json.Marshal(map[string]any{"text": strings.Repeat("payload", 8192), "usage": map[string]any{"tokens": 7}})
	err = db.Write(tb.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "plugins.connections", persistence.Key(catalog.Scope, connection.ID), connection); err != nil {
			return err
		}
		for i := range 100 {
			id := core.ID(fmt.Sprintf("call-%03d", i))
			call := HTTPCall{ID: id, OwnerID: p.OwnerID, PluginID: connection.PluginID, Connection: connection, ProviderID: "provider", State: "succeeded", CreatedAt: time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC), Result: payload, Arguments: json.RawMessage(`{"query":"private request"}`), Settings: map[string]any{"private_setting": "not listed"}, StatusCode: 200, DurationMS: 12}
			if err := tx.Put(ctx, "plugins.http-calls", persistence.Key(p.OwnerID, id), call); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	observed := &historyObservedStore{Store: db}
	service := New(observed, Options{Authorize: func(context.Context, core.Principal, bool) error { return nil }})
	return service, p, ConnectionRequest{PluginID: connection.PluginID, ID: connection.ID}, observed
}
func TestHTTPHistoryReadsOnlySummaryFields(t *testing.T) {
	s, p, q, observed := historyProjectionFixture(t)
	rows, err := s.HTTPHistory(t.Context(), p, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 100 || rows[0].ID != "call-099" || rows[99].ID != "call-000" {
		t.Fatal("history ordering or limit changed")
	}
	for _, row := range rows {
		if row.Usage.(map[string]any)["tokens"] != float64(7) {
			t.Fatal("usage missing", row)
		}
	}
	if observed.bytes > 128<<10 {
		t.Fatalf("100 history summaries read %d bytes including full request/result payloads", observed.bytes)
	}
}
func BenchmarkHTTPHistorySummary(b *testing.B) {
	s, p, q, _ := historyProjectionFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		rows, err := s.HTTPHistory(b.Context(), p, q)
		if err != nil || len(rows) != 100 {
			b.Fatalf("history rows=%d err=%v", len(rows), err)
		}
	}
}

func TestHTTPHistoryProjectionPreservesUsageTypesAndMetadata(t *testing.T) {
	s, p, q, _ := historyProjectionFixture(t)
	usages := []any{nil, true, false, "中文\nquoted", float64(7), []any{true, "x", float64(2)}, map[string]any{"tokens": float64(3), "cached": false}}
	want := map[core.ID]HTTPCallView{}
	must(t, s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i, usage := range usages {
			id := core.ID(fmt.Sprintf("typed-%d", i))
			at := time.Date(2026, 2, 1, 0, 0, i, 123, time.UTC)
			raw, _ := json.Marshal(map[string]any{"usage": usage, "text": "private result"})
			call := HTTPCall{ID: id, OwnerID: p.OwnerID, ProviderID: "typed-provider", Connection: Connection{ID: q.ID}, State: "unknown", CreatedAt: at, DurationMS: 17, StatusCode: 503, Failure: "timeout", Test: i%2 == 0, Result: raw}
			want[id] = HTTPCallView{ID: id, OwnerID: p.OwnerID, ProviderID: call.ProviderID, ConnectionID: q.ID, State: call.State, CreatedAt: at, DurationMS: 17, StatusCode: 503, Failure: "timeout", Test: call.Test, Usage: usage}
			if err := tx.Put(ctx, "plugins.http-calls", persistence.Key(p.OwnerID, id), call); err != nil {
				return err
			}
		}
		return tx.Put(ctx, "plugins.http-calls", "owner/foreign", HTTPCall{ID: "foreign", OwnerID: p.OwnerID, Connection: Connection{ID: "other"}, CreatedAt: time.Now()})
	}))
	rows, err := s.HTTPHistory(t.Context(), p, q)
	must(t, err)
	if len(rows) != 100 {
		t.Fatal("query lost 100-row bound", len(rows))
	}
	for _, row := range rows {
		if row.ID == "foreign" {
			t.Fatal("cross-connection record")
		}
		if expected, ok := want[row.ID]; ok {
			if !reflect.DeepEqual(row, expected) {
				t.Fatalf("got %+v want %+v", row, expected)
			}
			delete(want, row.ID)
		}
	}
	if len(want) != 0 {
		t.Fatal("typed rows missing", want)
	}
	wrong := q
	wrong.PluginID = "another.plugin"
	if _, err := s.HTTPHistory(t.Context(), p, wrong); core.CodeOf(err) != core.NotFound {
		t.Fatal("wrong plugin accepted", err)
	}
	s.options.Authorize = func(context.Context, core.Principal, bool) error { return core.NewError(core.Forbidden, "denied") }
	if _, err := s.HTTPHistory(t.Context(), p, q); core.CodeOf(err) != core.Forbidden {
		t.Fatal("authorization bypassed", err)
	}
}
