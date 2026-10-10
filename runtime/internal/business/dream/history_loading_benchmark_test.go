package dream

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func BenchmarkRunHistory(b *testing.B) {
	db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "runtime.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	err = db.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, sessions, "owner/session", Session{ID: "session", OwnerID: "owner", AssistantID: "assistant", State: "completed", Sources: []Evidence{{Text: strings.Repeat("e", 8192)}}}); err != nil {
			return err
		}
		for i := 0; i < 1000; i++ {
			id := core.ID(fmt.Sprintf("run-%04d", i))
			if err := tx.Put(ctx, runs, persistence.Key("owner", id), Run{ID: id, OwnerID: "owner", AssistantID: "assistant", SessionID: "session", CreatedAt: now.Add(time.Duration(i) * time.Second), Output: strings.Repeat("x", 4096)}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	s := New(db, Options{Authorizer: allow{}})
	p := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		rows, err := s.Runs(b.Context(), p, "assistant")
		if err != nil || len(rows) != 60 {
			b.Fatalf("rows=%d err=%v", len(rows), err)
		}
	}
}
