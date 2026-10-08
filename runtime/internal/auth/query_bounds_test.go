package auth

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
	"path/filepath"
	"testing"
)

func TestCredentialLookupsAndSessionPagesBoundHistory(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "auth.db"))
	testkit.RequireNoError(t, err)
	defer db.Close()
	testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < 1000; i++ {
			id := core.ID(fmt.Sprintf("%04d", i))
			owner := core.ID("owner")
			if i%2 == 0 {
				owner = "other"
			}
			if err := tx.Put(ctx, "auth.sessions", persistence.Key(owner, id), StoredSession{Session: Session{ID: id, AccountID: owner}, TokenDigest: string(id)}); err != nil {
				return err
			}
			if err := tx.Put(ctx, "auth.tokens", persistence.Key(owner, id), StoredToken{AccountID: owner, Token: Token{ID: id}, Digest: string(id)}); err != nil {
				return err
			}
		}
		return nil
	}))
	observed := &testkit.ObservedStore{Store: db}
	repo := NewRepository(observed)
	session, err := repo.FindSession(t.Context(), "0999")
	testkit.RequireNoError(t, err)
	if session.Session.AccountID != "owner" {
		t.Fatal(session)
	}
	token, err := repo.FindToken(t.Context(), "0999")
	testkit.RequireNoError(t, err)
	if token.AccountID != "owner" {
		t.Fatal(token)
	}
	_, err = repo.FindSession(t.Context(), "absent")
	testkit.RequireCode(t, err, core.NotFound)
	if observed.Scans != 0 || observed.Rows["auth.sessions"] != 1 || observed.Rows["auth.tokens"] != 1 {
		t.Fatalf("history loaded: %+v", observed)
	}
	page, err := repo.Sessions(t.Context(), "owner", core.PageRequest{Limit: 5})
	testkit.RequireNoError(t, err)
	next, err := repo.Sessions(t.Context(), "owner", core.PageRequest{Limit: 5, Cursor: page.NextCursor})
	testkit.RequireNoError(t, err)
	if len(page.Items) != 5 || len(next.Items) != 5 || page.Items[4].ID >= next.Items[0].ID || observed.Rows["auth.sessions"] != 13 {
		t.Fatalf("unbounded or overlapping pages: %+v", observed)
	}
	_, err = repo.Sessions(t.Context(), "other", core.PageRequest{Limit: 5, Cursor: page.NextCursor})
	testkit.RequireCode(t, err, core.InvalidArgument)
}

func BenchmarkSessionLookupHistory(b *testing.B) {
	for _, n := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "auth.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			err = db.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
				for i := 0; i < n; i++ {
					id := core.ID(fmt.Sprintf("session-%06d", i))
					if err := tx.Put(ctx, "auth.sessions", persistence.Key("owner", id), StoredSession{Session: Session{ID: id, AccountID: "owner"}, TokenDigest: string(id)}); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				b.Fatal(err)
			}
			repo := NewRepository(db)
			target := fmt.Sprintf("session-%06d", n-1)
			b.Run("indexed", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := repo.FindSession(b.Context(), target); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("scan", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					rows, err := persistence.Scan[StoredSession](b.Context(), db, "auth.sessions")
					if err != nil {
						b.Fatal(err)
					}
					found := false
					for _, row := range rows {
						if row.TokenDigest == target {
							found = true
							break
						}
					}
					if !found {
						b.Fatal("missing target")
					}
				}
			})
		})
	}
}
