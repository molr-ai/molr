package sqlitestore_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

func TestOwnerRecordBytesScopesAndTransactions(t *testing.T) {
	s := openStore(t)
	ctx := t.Context()
	rows := []struct {
		bucket, key string
		value       any
		own         bool
	}{
		{"test", "alice/record", map[string]any{"Text": "私有内容"}, true},
		{"test", "opaque", map[string]any{"OwnerID": "alice", "Text": "owned"}, true},
		{"auth.sessions", "session", map[string]any{"Session": map[string]any{"AccountID": "alice"}}, true},
		{"auth.accounts", "alice", map[string]any{"Account": map[string]any{"ID": "alice"}}, true},
		{"test", "account", map[string]any{"AccountID": "alice"}, true},
		{"test", "bob/record", map[string]any{"OwnerID": "bob"}, false},
		{"test", "alice-other/record", map[string]any{"Text": "other"}, false},
		{"context.objects", "shared", map[string]any{"OwnerID": "alice", "Data": "shared context counted separately"}, false},
		{"test", "shared", map[string]any{"Text": "shared"}, false},
	}
	var expected int64
	if err := s.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		for _, r := range rows {
			if err := tx.Put(ctx, r.bucket, r.key, r.value); err != nil {
				return err
			}
			if r.own {
				var raw json.RawMessage
				if err := tx.Get(ctx, r.bucket, r.key, &raw); err != nil {
					return err
				}
				expected += int64(len(raw))
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.OwnerRecordBytes(ctx, "alice"); err != nil || got != expected {
		t.Fatal(got, expected, err)
	}
	if _, err := s.OwnerRecordBytes(ctx, ""); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal(err)
	}
	rollback := errors.New("rollback")
	err := s.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "test", "alice/new", map[string]any{"Value": "new"}); err != nil {
			return err
		}
		got, err := s.OwnerRecordBytes(ctx, "alice")
		if err != nil {
			return err
		}
		if got <= expected {
			t.Fatal("nested transaction not observed", got)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if got, err := s.OwnerRecordBytes(ctx, "alice"); err != nil || got != expected {
		t.Fatal(got, expected, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.OwnerRecordBytes(canceled, "alice"); core.CodeOf(err) != core.Canceled {
		t.Fatal(err)
	}
}
