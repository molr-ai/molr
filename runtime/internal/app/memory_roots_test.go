package app

import (
	"context"
	"encoding/json"
	"fmt"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/platform"
	"os"
	"path/filepath"
	"testing"
)

type memoryRootReadStore struct {
	persistence.Store
	assistantRows                                 int
	dataRoot                                      string
	inspectPayloads, unrelatedFields, failQueries bool
}
type memoryRootReadTx struct {
	persistence.Tx
	parent *memoryRootReadStore
}

func (s *memoryRootReadStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, &memoryRootReadTx{tx, s}) })
}
func (t *memoryRootReadTx) Store() persistence.Store { return t.parent }
func (t *memoryRootReadTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	if bucket == "business.assistants" && t.parent.failQueries {
		return nil, core.NewError(core.Unavailable, "assistant query unavailable")
	}
	rows, err := t.Tx.Query(ctx, bucket, q)
	if bucket == "business.assistants" {
		t.parent.assistantRows += len(rows)
		if t.parent.inspectPayloads {
			for _, row := range rows {
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(row.Data, &fields); err != nil {
					return nil, err
				}
				if len(fields) != 2 || fields["ID"] == nil || fields["OwnerID"] == nil {
					t.parent.unrelatedFields = true
				}
			}
		}
	}
	return rows, err
}
func memoryRootsFixture(tb testing.TB) (*memory.Files, *memoryRootReadStore) {
	tb.Helper()
	root := tb.TempDir()
	db, err := sqlitestore.Open(filepath.Join(root, "memory.db"))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { db.Close() })
	err = db.Write(tb.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := range 250 {
			a := assistant.Assistant{ID: core.ID(fmt.Sprintf("assistant-%03d", i)), OwnerID: "owner", Name: fmt.Sprintf("Memory assistant %03d", i), State: assistant.Active, Version: 1}
			if err := tx.Put(ctx, "business.assistants", persistence.Key(a.OwnerID, a.ID), a); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	global := platform.UserGlobal(root, "owner")
	if err := os.MkdirAll(global, 0700); err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(global, "MEMORY.md"), []byte("Global memory fixture"), 0600); err != nil {
		tb.Fatal(err)
	}
	personal := platform.AssistantWorkspace(root, "", "owner", "assistant-249")
	if err := os.MkdirAll(personal, 0700); err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(personal, "MEMORY.md"), []byte("Assistant memory fixture"), 0600); err != nil {
		tb.Fatal(err)
	}
	observed := &memoryRootReadStore{Store: db, dataRoot: root}
	files := memory.NewFiles(memory.FileOptions{Roots: func(ctx context.Context, owner core.ID, query memory.RootQuery) ([]memory.Root, error) {
		return runtimeMemoryRoots(ctx, observed, root, "", owner, query)
	}})
	return files, observed
}
func TestGlobalMemoryReadDoesNotLoadAssistantRecords(t *testing.T) {
	files, store := memoryRootsFixture(t)
	value, err := files.Get(t.Context(), "owner", memory.Ref{Scope: memory.Global, Path: "MEMORY.md"})
	need(t, err)
	if value.Text != "Global memory fixture" {
		t.Fatal("wrong memory", value.Text)
	}
	t.Logf("one global file loaded %d assistant records", store.assistantRows)
	if store.assistantRows != 0 {
		t.Fatal("global memory resolution loaded unrelated assistants", store.assistantRows)
	}
}
func BenchmarkGlobalMemoryRootRead(b *testing.B) {
	files, store := memoryRootsFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, err := files.Get(b.Context(), "owner", memory.Ref{Scope: memory.Global, Path: "MEMORY.md"})
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(store.assistantRows)/float64(b.N), "assistant-records/op")
}

func TestAssistantMemoryLoadsOnlyTargetIdentity(t *testing.T) {
	files, store := memoryRootsFixture(t)
	store.inspectPayloads = true
	ref := memory.Ref{Scope: memory.Assistant, AssistantID: "assistant-249", Path: "MEMORY.md"}
	value, err := files.Get(t.Context(), "owner", ref)
	need(t, err)
	if value.Text != "Assistant memory fixture" || store.assistantRows != 1 || store.unrelatedFields {
		t.Fatal("single root loaded unrelated data", store.assistantRows, store.unrelatedFields)
	}
	need(t, store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		var a assistant.Assistant
		if err := tx.Get(ctx, "business.assistants", "owner/assistant-249", &a); err != nil {
			return err
		}
		a.State = assistant.Dismissed
		return tx.Put(ctx, "business.assistants", "owner/assistant-249", a)
	}))
	store.assistantRows = 0
	value, err = files.Get(t.Context(), "owner", ref)
	need(t, err)
	if value.Text != "Assistant memory fixture" || store.assistantRows != 1 {
		t.Fatal("dismissed assistant root disappeared")
	}
	if _, err = files.Get(t.Context(), "foreign", ref); core.CodeOf(err) != core.NotFound {
		t.Fatal("foreign account resolved owner memory", err)
	}
	ref.AssistantID = "missing"
	if _, err = files.Get(t.Context(), "owner", ref); core.CodeOf(err) != core.NotFound {
		t.Fatal("missing assistant root available", err)
	}
}

func TestMemoryListsNarrowRootsAndAllRootsProjectOnlyIdentity(t *testing.T) {
	files, store := memoryRootsFixture(t)
	store.inspectPayloads = true
	global, err := files.List(t.Context(), "owner", memory.Query{Scope: memory.Global})
	need(t, err)
	if len(global.Items) != 1 || global.Items[0].Scope != memory.Global || store.assistantRows != 0 {
		t.Fatal("global list read assistant records", len(global.Items), store.assistantRows)
	}
	personal, err := files.List(t.Context(), "owner", memory.Query{AssistantID: "assistant-249"})
	need(t, err)
	if len(personal.Items) != 1 || personal.Items[0].AssistantID != "assistant-249" || store.assistantRows != 1 {
		t.Fatal("scoped list expanded unrelated roots", len(personal.Items), store.assistantRows)
	}
	store.assistantRows = 0
	roots, err := runtimeMemoryRoots(t.Context(), store, store.dataRoot, "", "owner", memory.RootQuery{})
	need(t, err)
	if len(roots) != 251 || store.assistantRows != 250 || store.unrelatedFields {
		t.Fatal("all-root discovery lost roots or loaded profiles", len(roots), store.assistantRows, store.unrelatedFields)
	}
	all, err := files.List(t.Context(), "owner", memory.Query{})
	need(t, err)
	if len(all.Items) != 2 {
		t.Fatal("unscoped list omitted memories", len(all.Items))
	}
}

func TestMemoryExportKeepsEveryRoot(t *testing.T) {
	files, store := memoryRootsFixture(t)
	store.inspectPayloads = true
	exported := map[string]string{}
	need(t, files.Export(t.Context(), "owner", func(name string, data []byte) error {
		exported[name] = string(data)
		return nil
	}))
	if len(exported) != 2 || exported["global/MEMORY.md"] != "Global memory fixture" || exported["assistants/assistant-249/MEMORY.md"] != "Assistant memory fixture" || store.assistantRows != 250 || store.unrelatedFields {
		t.Fatal("export lost root contents or loaded profiles", exported, store.assistantRows, store.unrelatedFields)
	}
}

func TestGlobalMemoryDoesNotDependOnAssistantQueries(t *testing.T) {
	files, store := memoryRootsFixture(t)
	store.failQueries = true
	value, err := files.Get(t.Context(), "owner", memory.Ref{Scope: memory.Global, Path: "MEMORY.md"})
	need(t, err)
	if value.Text != "Global memory fixture" {
		t.Fatal(value.Text)
	}
	if _, err = files.Get(t.Context(), "owner", memory.Ref{Scope: memory.Assistant, AssistantID: "assistant-249", Path: "MEMORY.md"}); core.CodeOf(err) != core.Unavailable {
		t.Fatal("assistant discovery error swallowed", err)
	}
	if _, err = files.List(t.Context(), "owner", memory.Query{}); core.CodeOf(err) != core.Unavailable {
		t.Fatal("all-root discovery error swallowed", err)
	}
}
