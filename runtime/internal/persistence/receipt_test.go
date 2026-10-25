package persistence_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type failedReceiptRead struct {
	persistence.Tx
	failure error
}

func (tx failedReceiptRead) Get(context.Context, string, string, any) error { return tx.failure }

func TestReceiptReplayPreservesScopeDigestAndErrors(t *testing.T) {
	store, ctx := writeStore(t), t.Context()
	digest, err := persistence.PayloadDigest(map[string]any{"name": "original", "version": 1})
	if err != nil || len(digest) != 64 {
		t.Fatal(digest, err)
	}
	again, err := persistence.PayloadDigest(map[string]any{"version": 1, "name": "original"})
	if err != nil || again != digest {
		t.Fatal("JSON key order changed identity", err)
	}
	changed, err := persistence.PayloadDigest(map[string]any{"name": "changed", "version": 1})
	if err != nil || changed == digest {
		t.Fatal("different payload reused identity", err)
	}
	if _, err := persistence.PayloadDigest(func() {}); err == nil {
		t.Fatal("unencodable payload accepted")
	}
	key := persistence.Key("owner", "create/object/request")
	err = store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var result versionedRecord
		if found, err := persistence.ReplayReceipt(ctx, tx, "receipts", key, digest, &result); found || err != nil {
			t.Fatal(found, err)
		}
		if err := tx.Put(ctx, "receipts", key, persistence.Receipt{Digest: digest, Result: json.RawMessage(`{"Version":1,"Value":"original result"}`)}); err != nil {
			return err
		}
		if found, err := persistence.ReplayReceipt(ctx, tx, "receipts", key, digest, &result); !found || err != nil || result != (versionedRecord{1, "original result"}) {
			t.Fatal(found, result, err)
		}
		if _, err := persistence.ReplayReceipt(ctx, tx, "receipts", key, changed, &result); core.CodeOf(err) != core.Conflict {
			t.Fatal(err)
		}
		for _, other := range []string{persistence.Key("other", "create/object/request"), persistence.Key("owner", "update/object/request")} {
			if found, err := persistence.ReplayReceipt(ctx, tx, "receipts", other, digest, &result); found || err != nil {
				t.Fatal("receipt escaped scope", found, err)
			}
		}
		if err := tx.Put(ctx, "receipts", "bad-result", persistence.Receipt{Digest: digest, Result: json.RawMessage(`"wrong result type"`)}); err != nil {
			return err
		}
		if found, err := persistence.ReplayReceipt(ctx, tx, "receipts", "bad-result", digest, &result); !found || err == nil {
			t.Fatal("invalid result treated as a new request", found, err)
		}
		failure := errors.New("read unavailable")
		if _, err := persistence.ReplayReceipt(ctx, failedReceiptRead{tx, failure}, "receipts", key, digest, &result); !errors.Is(err, failure) {
			t.Fatal("read error lost", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReceiptAndBusinessWritesRollbackTogether(t *testing.T) {
	store, ctx := writeStore(t), t.Context()
	digest, err := persistence.PayloadDigest("request")
	if err != nil {
		t.Fatal(err)
	}
	write := func() error {
		return store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			_, err := persistence.MutateReceipt(ctx, tx, "receipts", "request", digest, func(ctx context.Context) (int, error) {
				return 7, persistence.Insert(ctx, tx.Store(), "business", "object", 7)
			})
			return err
		})
	}
	store.FailNextCommit(errors.New("commit failed"))
	if err := write(); err == nil {
		t.Fatal("commit failure lost")
	}
	if _, err := persistence.Load[int](ctx, store, "business", "object"); !persistence.Missing(err) {
		t.Fatal("partial business write", err)
	}
	if err := write(); err != nil {
		t.Fatal("retry failed", err)
	}
	err = store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var result int
		found, err := persistence.ReplayReceipt(ctx, tx, "receipts", "request", digest, &result)
		if !found || result != 7 {
			t.Fatal(found, result)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMutateReceiptReplaysAndPropagatesFailures(t *testing.T) {
	store := writeStore(t)
	calls := 0
	write := func(key, digest string) error {
		return store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			value, err := persistence.MutateReceipt(ctx, tx, "receipts", key, digest, func(context.Context) (int, error) {
				calls++
				return 7, nil
			})
			if err == nil && value != 7 {
				t.Fatalf("lost mutation result: %d", value)
			}
			return err
		})
	}
	for range 2 {
		if err := write("owner/create", "original"); err != nil {
			t.Fatal(err)
		}
	}
	if err := write("owner/create", "changed"); core.CodeOf(err) != core.Conflict || calls != 1 {
		t.Fatal("replay/conflict executed mutation", calls, err)
	}
	if err := write("other/create", "original"); err != nil || calls != 2 {
		t.Fatal("scope was not independent", calls, err)
	}
	failure := errors.New("mutation failed")
	for _, encodeFailure := range []bool{false, true} {
		err := store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			_, err := persistence.MutateReceipt(ctx, tx, "receipts", "failed", "digest", func(ctx context.Context) (any, error) {
				if err := tx.Put(ctx, "business", "failed", 1); err != nil {
					return nil, err
				}
				if encodeFailure {
					return func() {}, nil
				}
				return nil, failure
			})
			return err
		})
		if err == nil || !encodeFailure && !errors.Is(err, failure) {
			t.Fatal("mutation/encoding error lost", err)
		}
		if _, err := persistence.Load[int](t.Context(), store, "business", "failed"); !persistence.Missing(err) {
			t.Fatal("failed mutation persisted business data", err)
		}
	}
	if err := write("failed", "digest"); err != nil || calls != 3 {
		t.Fatal("failed mutation persisted receipt", calls, err)
	}
}
