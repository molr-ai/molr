package app

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/search"
)

func TestFileMemoriesCloneSearchExportAndCleanup(t *testing.T) {
	ctx := t.Context()
	cfg := testConfig(t)
	b, err := Build(ctx, cfg)
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	a := dataAssistant(t, b, p)
	m, err := b.Services.Memories.Create(ctx, p, memory.CreateRequest{Text: "unique-file-preference", Mutation: memory.Mutation{IdempotencyKey: "file-memory"}, Ref: memory.Ref{Scope: memory.Assistant, AssistantID: a.ID, Path: ".agents/memory/file-memory.md"}})
	need(t, err)
	name := filepath.Join(platform.AssistantWorkspace(cfg.DataDir, cfg.Sandbox.WorkRoot, p.OwnerID, a.ID), strings.TrimPrefix(m.Path, "/work/"))
	raw, err := os.ReadFile(name)
	need(t, err)
	clone, err := b.Services.Assistants.Clone(ctx, p, assistant.CloneRequest{SourceID: a.ID, Name: "Copy", CopyMemory: true, Mutation: core.Mutation{IdempotencyKey: "clone-files"}})
	need(t, err)
	page, err := b.Services.Memories.List(ctx, p, memory.Query{AssistantID: clone.ID})
	need(t, err)
	if len(page.Items) != 1 || page.Items[0].Text != m.Text {
		t.Fatal("clone did not copy file", page)
	}
	hits, err := b.Services.Search.Search(ctx, p, search.Query{Text: m.Text, Sources: []search.Source{search.Memories}})
	need(t, err)
	if len(hits.Hits.Items) != 2 {
		t.Fatal("live files not searchable", hits)
	}
	_, err = b.Services.Index.Rebuild(ctx, p)
	need(t, err)
	docs, err := persistence.Scan[search.Document](ctx, b.store, "search.documents")
	need(t, err)
	for _, d := range docs {
		if d.Source == search.Memories {
			t.Fatal("memory body persisted in search index")
		}
	}
	proof, err := b.Services.Auth.Reauthenticate(ctx, p, auth.ReauthenticateRequest{Purpose: "export", Password: core.NewSecret("correct-horse-battery-staple-2026!")})
	need(t, err)
	exported, err := b.Services.Settings.Export(ctx, p, settings.ExportRequest{Mutation: core.Mutation{IdempotencyKey: "export-files"}, Verification: proof})
	need(t, err)
	content, err := b.Services.Files.Open(ctx, p, exported.FileID)
	need(t, err)
	data, err := io.ReadAll(content.Body)
	content.Body.Close()
	need(t, err)
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	need(t, err)
	found := 0
	for _, entry := range archive.File {
		if strings.HasPrefix(entry.Name, "memory/assistants/") {
			found++
			reader, err := entry.Open()
			need(t, err)
			body, err := io.ReadAll(reader)
			reader.Close()
			need(t, err)
			if !strings.Contains(string(body), m.Text) {
				t.Fatal("archive lost Markdown body")
			}
		}
	}
	if found != 2 {
		t.Fatal("archive missing memory files", found)
	}
	proof, err = b.Services.Auth.Reauthenticate(ctx, p, auth.ReauthenticateRequest{Purpose: "cleanup", Password: core.NewSecret("correct-horse-battery-staple-2026!")})
	need(t, err)
	req := settings.CleanupRequest{Mutation: core.Mutation{IdempotencyKey: "cleanup-file"}, Verification: proof, MemoryFiles: []memory.Ref{m.Ref}}
	need(t, b.Services.Settings.Cleanup(ctx, p, req))
	need(t, b.Services.Settings.Cleanup(ctx, p, req))
	if _, err = os.Stat(name); !os.IsNotExist(err) {
		t.Fatal("cleanup did not remove file", err)
	}
	need(t, os.WriteFile(name, raw, 0600))
	if err = b.Services.Settings.Cleanup(ctx, p, req); core.CodeOf(err) != core.Conflict {
		t.Fatal("cleanup replay deleted recreated file", err)
	}
	if _, err = os.Stat(name); err != nil {
		t.Fatal("recreated memory missing", err)
	}
	rows, err := persistence.Scan[memory.Memory](ctx, b.store, "business.memories")
	need(t, err)
	if len(rows) != 0 {
		t.Fatal("DB memory records written")
	}
}
