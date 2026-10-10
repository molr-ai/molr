package app

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func BenchmarkPermissionHistoryContext(b *testing.B) {
	db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "history.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	err = db.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < 5000; i++ {
			id := core.ID(fmt.Sprintf("request-%05d", i))
			state := "rejected"
			if i < 3 {
				state = "pending"
			}
			row := policy.PermissionRequest{ID: id, OwnerID: "owner", AssistantID: "assistant", State: state, Version: 1, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, i, time.UTC), Input: policy.PermissionRequestInput{Reason: strings.Repeat("x", 1024)}}
			if err := tx.Put(ctx, permissionRequestsBucket, persistence.Key("owner", id), row); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	s := &permissionRequests{store: db, authorizer: providerTestPolicy{}}
	e := core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "execution"}, AssistantID: "assistant"}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := s.context(b.Context(), e); err != nil {
			b.Fatal(err)
		}
	}
}
