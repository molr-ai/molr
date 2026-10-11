package sqlitestore_test

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"

	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func BenchmarkStoreOpen(b *testing.B) {
	dir := b.TempDir()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s, err := sqlitestore.Open(filepath.Join(dir, strconv.Itoa(i)+".db"))
		if err != nil {
			b.Fatal(err)
		}
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRecordWrites(b *testing.B) {
	s, err := sqlitestore.Open(filepath.Join(b.TempDir(), "writes.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
			for j := range 1000 {
				if err := tx.Put(ctx, "context.counts", strconv.Itoa(j), struct{ Count int }{i}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			b.Fatal(err)
		}
	}
}
