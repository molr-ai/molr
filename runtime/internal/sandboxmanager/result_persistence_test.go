package sandboxmanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func TestCompletedResultSurvivesTransientCommitFailure(t *testing.T) {
	backend := &testBackend{started: make(chan struct{}, 1), release: make(chan struct{})}
	manager := managerFixture(t, backend)
	request := testRequest()
	if _, err := manager.Submit(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-backend.started:
	case <-time.After(time.Second):
		t.Fatal("backend did not start")
	}
	manager.Store.(*sqlitestore.Store).FailNextCommit(errors.New("temporary result commit failure"))
	close(backend.release)
	result := awaitReply(t, manager, request.Identity)
	if result.State != "succeeded" || string(result.Result) != `{"ok":true}` {
		t.Fatalf("lost completed result: %+v", result)
	}
	if backend.calls.Load() != 1 {
		t.Fatalf("backend executed %d times, want exactly once", backend.calls.Load())
	}
	duplicate, err := manager.Submit(t.Context(), request)
	if err != nil || duplicate.State != "succeeded" {
		t.Fatalf("deduplicated result: %+v, %v", duplicate, err)
	}
	if backend.calls.Load() != 1 {
		t.Fatal("result persistence retried the external effect")
	}
}

type resultFaultStore struct {
	persistence.Store
	fail     atomic.Bool
	attempts atomic.Int32
}
type resultFaultTx struct {
	persistence.Tx
	store *resultFaultStore
}

var resultCommitError = errors.New("result persistence unavailable")

func (s *resultFaultStore) Write(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, resultFaultTx{tx, s}) })
}
func (tx resultFaultTx) Put(ctx context.Context, bucket, key string, value any) error {
	if r, ok := value.(record); ok && bucket == operationsBucket && r.Reply.State != "running" {
		tx.store.attempts.Add(1)
		if tx.store.fail.Load() {
			return resultCommitError
		}
	}
	return tx.Tx.Put(ctx, bucket, key, value)
}

type resultBackend struct {
	testBackend
	failure error
	result  json.RawMessage
}

func (b *resultBackend) Call(ctx context.Context, q Request) (json.RawMessage, error) {
	result, err := b.testBackend.Call(ctx, q)
	if err != nil {
		return result, err
	}
	if b.failure != nil {
		return nil, b.failure
	}
	if b.result != nil {
		return b.result, nil
	}
	return result, nil
}
func resultFixture(t *testing.T, failure error) (*Manager, *resultFaultStore, *resultBackend) {
	t.Helper()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store := &resultFaultStore{Store: db}
	backend := &resultBackend{failure: failure}
	manager, err := New(t.Context(), store, backend)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })
	return manager, store, backend
}
func awaitResultAttempt(t *testing.T, store *resultFaultStore) {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		if store.attempts.Load() > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("result persistence was never attempted")
}

func TestResultRecoveryDoesNotRepeatEffects(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		failure     error
	}{{"success", "succeeded", nil}, {"failed", "failed", core.NewError(core.Conflict, "changed")}, {"unknown", "unknown", core.NewError(core.Unavailable, "outcome uncertain")}} {
		t.Run(tc.name, func(t *testing.T) {
			manager, store, backend := resultFixture(t, tc.failure)
			store.fail.Store(true)
			q := testRequest()
			if _, err := manager.Submit(t.Context(), q); err != nil {
				t.Fatal(err)
			}
			awaitResultAttempt(t, store)
			duplicate, err := manager.Submit(t.Context(), q)
			if err != nil || duplicate.State != "running" {
				t.Fatalf("pending durability: %+v %v", duplicate, err)
			}
			changed := q
			changed.Arguments = json.RawMessage(`{"text":"different"}`)
			if _, err := manager.Submit(t.Context(), changed); core.CodeOf(err) != core.Conflict {
				t.Fatalf("conflicting request accepted: %v", err)
			}
			other := q.Identity
			other.AssistantID = "other"
			if _, err := manager.Get(t.Context(), other); core.CodeOf(err) != core.Forbidden {
				t.Fatalf("result identity leaked: %v", err)
			}
			if err := manager.Cancel(t.Context(), q.Identity); err != nil {
				t.Fatal(err)
			}
			store.fail.Store(false)
			reply := awaitReply(t, manager, q.Identity)
			if reply.State != tc.state {
				t.Fatalf("reply = %+v, want %s", reply, tc.state)
			}
			if tc.failure != nil && reply.Code != core.CodeOf(tc.failure) {
				t.Fatalf("lost error: %+v", reply)
			}
			if backend.calls.Load() != 1 {
				t.Fatalf("effects = %d", backend.calls.Load())
			}
			if err := manager.Close(); err != nil {
				t.Fatal(err)
			}
			restarted, err := New(t.Context(), store, backend)
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close()
			reply, err = restarted.Submit(t.Context(), q)
			if err != nil || reply.State != tc.state || backend.calls.Load() != 1 {
				t.Fatalf("durable result lost after restart: %+v %v", reply, err)
			}
		})
	}
}

func TestCloseReportsUnpersistedResultAndRestartDoesNotReplay(t *testing.T) {
	manager, store, backend := resultFixture(t, nil)
	store.fail.Store(true)
	q := testRequest()
	if _, err := manager.Submit(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	awaitResultAttempt(t, store)
	closed := make(chan error, 1)
	go func() { closed <- manager.Close() }()
	select {
	case err := <-closed:
		if !errors.Is(err, resultCommitError) {
			t.Fatalf("close lost persistence error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("close stuck retrying persistence")
	}
	store.fail.Store(false)
	restarted, err := New(t.Context(), store, backend)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	reply, err := restarted.Submit(t.Context(), q)
	if err != nil || reply.State != "unknown" || reply.Reason != "sandbox_outcome_unknown" || backend.calls.Load() != 1 {
		t.Fatalf("unknown effect replayed: %+v %v, calls=%d", reply, err, backend.calls.Load())
	}
}

func TestPendingResultsKeepExecutionCapacityBounded(t *testing.T) {
	manager, store, backend := resultFixture(t, nil)
	store.fail.Store(true)
	for i := 0; i < 16; i++ {
		q := testRequest()
		q.Identity.OperationID = core.ID(fmt.Sprintf("operation-%d", i))
		if _, err := manager.Submit(t.Context(), q); err != nil {
			t.Fatal(err)
		}
	}
	awaitResultAttempt(t, store)
	q := testRequest()
	q.Identity.OperationID = "overflow"
	if _, err := manager.Submit(t.Context(), q); core.CodeOf(err) != core.RateLimited {
		t.Fatalf("pending results bypassed capacity: %v", err)
	}
	store.fail.Store(false)
	manager.wg.Wait()
	if _, err := manager.Submit(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	awaitReply(t, manager, q.Identity)
	if backend.calls.Load() != 17 {
		t.Fatalf("unexpected effects: %d", backend.calls.Load())
	}
}

func TestTLSClientObservesResultAfterCommitRetry(t *testing.T) {
	backend := &testBackend{started: make(chan struct{}, 1), release: make(chan struct{})}
	manager := managerFixture(t, backend)
	token := core.NewSecret(strings.Repeat("t", 48))
	handler, err := Handler(manager, token)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(handler)
	defer server.Close()
	fingerprint := sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)
	client, err := NewClient(server.URL, hex.EncodeToString(fingerprint[:]), token, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	q := testRequest()
	finished := make(chan error, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go func() {
		value, err := client.Execute(ctx, q, func(context.Context) error { return nil })
		if err == nil && string(value) != `{"ok":true}` {
			err = fmt.Errorf("unexpected reply: %s", value)
		}
		finished <- err
	}()
	select {
	case <-backend.started:
	case <-ctx.Done():
		t.Fatal("backend did not start")
	}
	manager.Store.(*sqlitestore.Store).FailNextCommit(resultCommitError)
	close(backend.release)
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("client never observed durable completion")
	}
	if backend.calls.Load() != 1 {
		t.Fatal("HTTP recovery repeated the effect")
	}
}

func TestResultTooLargeForLedgerBecomesUnknownWithoutReplay(t *testing.T) {
	manager, _, backend := resultFixture(t, nil)
	// Escaping HTML in JSON expands this protocol-valid result beyond the 32 MiB record limit.
	backend.result = json.RawMessage(`"` + strings.Repeat("<", 6<<20) + `"`)
	q := testRequest()
	if _, err := manager.Submit(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	var reply Reply
	for time.Now().Before(deadline) {
		var err error
		reply, err = manager.Get(t.Context(), q.Identity)
		if err != nil {
			t.Fatal(err)
		}
		if reply.State != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if reply.State != "unknown" || reply.Reason != "sandbox_invalid_result" {
		t.Fatalf("unpersistable reply did not terminate: state=%s reason=%s", reply.State, reply.Reason)
	}
	duplicate, err := manager.Submit(t.Context(), q)
	if err != nil || duplicate.State != "unknown" || backend.calls.Load() != 1 {
		t.Fatalf("invalid result replayed: %+v %v", duplicate, err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
}
