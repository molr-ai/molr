package sqlitestore

import (
	"context"
	"errors"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"path/filepath"
	"testing"
)

func TestEncodedBytesRespectsScopeAndOriginalEncoding(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "sizes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	err = s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for key, value := range map[string]any{
			"alice/1": map[string]any{"Text": "中文🙂", "Rank": 3},
			"alice/2": map[string]any{"Text": "<escaped>&\n", "Rank": 1},
			"alice/3": map[string]any{"Text": "different length", "Rank": 2},
			"bob/1":   map[string]any{"Text": "foreign", "Rank": 4},
		} {
			if err := tx.Put(ctx, "sizes", key, value); err != nil {
				return err
			}
		}
		return tx.Put(ctx, "another", "alice/1", map[string]any{"Text": "another bucket"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []persistence.Query{
		{}, {Prefix: "alice/"}, {Prefix: "missing/"},
		{Prefix: "alice/", OrderBy: "Rank", Descending: true, Limit: 1, Fields: []string{"Rank"}},
		{Where: []persistence.Filter{{Field: "Rank", Op: ">", Value: 1}}, OrderBy: "Rank", After: []any{2, "alice/3"}, Limit: 1},
	} {
		err = s.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			got, err := tx.EncodedBytes(ctx, "sizes", q)
			if err != nil {
				return err
			}
			full := q
			full.Fields = nil
			rows, err := tx.Query(ctx, "sizes", full)
			if err != nil {
				return err
			}
			var want int64
			for _, r := range rows {
				want += int64(len(r.Data))
			}
			if got != want {
				t.Fatalf("query=%+v got=%d want=%d", q, got, want)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	err = s.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		_, err := tx.EncodedBytes(canceled, "sizes", persistence.Query{})
		return err
	})
	if core.CodeOf(err) != core.Canceled {
		t.Fatalf("cancellation lost: %v", err)
	}
}
func TestEncodedBytesObservesTransactionAndRollback(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "transaction.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	stop := errors.New("roll back")
	err = s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "sizes", "one", "中文"); err != nil {
			return err
		}
		got, err := tx.EncodedBytes(ctx, "sizes", persistence.Query{})
		if err != nil {
			return err
		}
		if got != 8 {
			t.Fatalf("UTF-8 JSON bytes=%d, want 8", got)
		}
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatal(err)
	}
	err = s.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		got, err := tx.EncodedBytes(ctx, "sizes", persistence.Query{})
		if got != 0 {
			t.Fatal("rolled-back bytes counted", got)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
