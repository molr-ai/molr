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
			var result int
			if found, err := persistence.ReplayReceipt(ctx, tx, "receipts", "request", digest, &result); found || err != nil {
				return errors.New("rolled-back receipt was retained")
			}
			if err := persistence.Insert(ctx, tx.Store(), "business", "object", 7); err != nil {
				return err
			}
			return tx.Put(ctx, "receipts", "request", persistence.Receipt{Digest: digest, Result: json.RawMessage(`7`)})
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
