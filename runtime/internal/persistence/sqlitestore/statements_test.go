package sqlitestore_test

import (
	"context"
	"encoding/json"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

func TestRecordStatementsRemainUsableAfterErrorsAndCancellation(t *testing.T) {
	s := openStore(t)
	ctx := t.Context()
	session := json.RawMessage(`{"Session":{"AccountID":"owner"},"DeviceDigest":"device"}`)
	if err := s.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "auth.sessions", "first", session); err != nil {
			return err
		}
		if err := tx.Put(ctx, "auth.sessions", "duplicate", session); core.CodeOf(err) != core.Conflict {
			t.Fatalf("unique constraint: %v", err)
		}
		if err := tx.Delete(ctx, "auth.sessions", "missing"); core.CodeOf(err) != core.NotFound {
			t.Fatalf("missing delete: %v", err)
		}
		if err := tx.Delete(ctx, "auth.sessions", "first"); err != nil {
			return err
		}
		return tx.Put(ctx, "auth.sessions", "replacement", session)
	}); err != nil {
		t.Fatal(err)
	}

	canceled, cancel := context.WithCancel(ctx)
	defer cancel()
	err := s.Write(canceled, func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Delete(ctx, "auth.sessions", "replacement"); err != nil {
			return err
		}
		if err := tx.Put(ctx, "test", "rolled-back", true); err != nil {
			return err
		}
		cancel()
		return nil
	})
	if core.CodeOf(err) != core.Canceled {
		t.Fatal(err)
	}
	// Cancellation may discard the connection. A later transaction must still
	// see rolled-back data and use all three record operations successfully.
	if err := s.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var raw json.RawMessage
		if err := tx.Get(ctx, "test", "rolled-back", &raw); core.CodeOf(err) != core.NotFound {
			t.Fatalf("canceled write escaped: %v", err)
		}
		if err := tx.Get(ctx, "auth.sessions", "replacement", &raw); err != nil {
			return err
		}
		if err := tx.Delete(ctx, "auth.sessions", "replacement"); err != nil {
			return err
		}
		return tx.Put(ctx, "auth.sessions", "after-cancel", session)
	}); err != nil {
		t.Fatal(err)
	}
}
