package event

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
)

func init() {
	newContractFixture = func(t *testing.T) contractFixture {
		t.Helper()
		ctx := testkit.Context(t)
		db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "events.db"))
		testkit.RequireNoError(t, err)
		ready := make(chan Writer, 1)
		decision := make(chan error, 1)
		done := make(chan struct{})
		var transactionErr error
		go func() {
			transactionErr = db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				ready <- NewWriter(tx.Store())
				select {
				case err := <-decision:
					return err
				case <-ctx.Done():
					return ctx.Err()
				}
			})
			close(done)
		}()
		var w Writer
		select {
		case w = <-ready:
		case <-done:
			testkit.RequireNoError(t, transactionErr)
			t.Fatal("transaction did not start")
		case <-ctx.Done():
			t.Fatal("transaction start timed out")
		}
		var once sync.Once
		rollback := errors.New("intentional rollback")
		finish := func(ctx context.Context, why error) error {
			once.Do(func() { decision <- why })
			select {
			case <-done:
				if errors.Is(transactionErr, rollback) {
					return nil
				}
				return transactionErr
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		t.Cleanup(func() { _ = finish(context.Background(), rollback); _ = db.Close() })
		return contractFixture{Writer: w, Commit: func(ctx context.Context) error { return finish(ctx, nil) }, Rollback: func(ctx context.Context) error { return finish(ctx, rollback) }, Events: func(ctx context.Context) ([]AssistantEvent, error) {
			return persistence.Scan[AssistantEvent](ctx, db, eventsBucket)
		}}
	}
}
