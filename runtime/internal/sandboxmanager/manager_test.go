package sandboxmanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
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

type testBackend struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (b *testBackend) Info(context.Context) (Info, error) {
	return Info{Protocol: ProtocolVersion, Enabled: true}, nil
}
func (b *testBackend) Call(ctx context.Context, q Request) (json.RawMessage, error) {
	b.calls.Add(1)
	if b.started != nil {
		select {
		case b.started <- struct{}{}:
		default:
		}
	}
	if b.release != nil {
		select {
		case <-b.release:
		case <-ctx.Done():
			return nil, core.NewError(core.Canceled, "stopped")
		}
	}
	return json.RawMessage(`{"ok":true}`), nil
}
func (b *testBackend) Cancel(context.Context, Identity) error { return nil }
func (b *testBackend) Close() error                           { return nil }
func managerFixture(t *testing.T, b *testBackend) *Manager {
	t.Helper()
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	m, err := New(t.Context(), store, b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	return m
}
func testRequest() Request {
	return Request{Identity: Identity{OwnerID: "owner", AssistantID: "assistant", ExecutionID: "execution", OperationID: "operation"}, Method: "file_write", Arguments: json.RawMessage(`{"text":"hello"}`), Deadline: time.Now().Add(time.Minute)}
}
func awaitReply(t *testing.T, m *Manager, id Identity) Reply {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		reply, err := m.Get(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if reply.State != "running" {
			return reply
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("operation never finished")
	return Reply{}
}
func TestDurableDeduplicationAndDigest(t *testing.T) {
	b := &testBackend{}
	m := managerFixture(t, b)
	q := testRequest()
	if _, err := m.Submit(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	if r := awaitReply(t, m, q.Identity); r.State != "succeeded" {
		t.Fatal(r)
	}
	q.Deadline = time.Now().Add(time.Minute)
	if r, err := m.Submit(t.Context(), q); err != nil || r.State != "succeeded" {
		t.Fatal(r, err)
	}
	if b.calls.Load() != 1 {
		t.Fatal("mutation executed twice")
	}
	q.Arguments = json.RawMessage(`{"text":"different"}`)
	if _, err := m.Submit(t.Context(), q); core.CodeOf(err) != core.Conflict {
		t.Fatal("digest reuse accepted", err)
	}
	other := q.Identity
	other.AssistantID = "other"
	if _, err := m.Get(t.Context(), other); core.CodeOf(err) != core.Forbidden {
		t.Fatal("operation leaked", err)
	}
}
func TestRevocationFencesLateRequestsAndCancelsRunning(t *testing.T) {
	b := &testBackend{started: make(chan struct{}, 1), release: make(chan struct{})}
	m := managerFixture(t, b)
	q := testRequest()
	if _, err := m.Submit(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	<-b.started
	if err := m.Renew(t.Context(), q.Identity); err != nil {
		t.Fatal(err)
	}
	if err := m.Revoke(t.Context(), q.Identity.OwnerID, 1); err != nil {
		t.Fatal(err)
	}
	if r := awaitReply(t, m, q.Identity); r.State != "unknown" {
		t.Fatal("cancellation claimed rollback", r)
	}
	q.Identity.OperationID = "late"
	if _, err := m.Submit(t.Context(), q); core.CodeOf(err) != core.Forbidden {
		t.Fatal("stale authorization accepted", err)
	}
}
func TestLeaseExpiryStopsWork(t *testing.T) {
	b := &testBackend{started: make(chan struct{}, 1), release: make(chan struct{})}
	m := managerFixture(t, b)
	q := testRequest()
	if _, err := m.Submit(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	<-b.started
	m.mu.Lock()
	m.active[operationKey(q.Identity)].lease = time.Now().Add(-time.Second)
	m.mu.Unlock()
	if r := awaitReply(t, m, q.Identity); r.State != "unknown" {
		t.Fatal(r)
	}
}
func TestRecoveryNeverReplaysUnknownEffects(t *testing.T) {
	b := &testBackend{}
	m := managerFixture(t, b)
	q := testRequest()
	if err := m.Store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, operationsBucket, operationKey(q.Identity), record{Identity: q.Identity, Reply: Reply{State: "running"}})
	}); err != nil {
		t.Fatal(err)
	}
	recovered, err := New(t.Context(), m.Store, b)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	r, err := recovered.Get(t.Context(), q.Identity)
	if err != nil || r.State != "unknown" || b.calls.Load() != 0 {
		t.Fatal(r, err)
	}
}
func TestTLSCredentialAndPinnedIdentity(t *testing.T) {
	m := managerFixture(t, &testBackend{})
	token := core.NewSecret(strings.Repeat("a", 48))
	handler, err := Handler(m, token)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(handler)
	defer server.Close()
	sum := sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)
	client, err := NewClient(server.URL, hex.EncodeToString(sum[:]), token, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Info(t.Context()); err != nil {
		t.Fatal(err)
	}
	q := testRequest()
	out, err := client.Execute(t.Context(), q, func(context.Context) error { return nil })
	if err != nil || string(out) != `{"ok":true}` {
		t.Fatal(string(out), err)
	}
	changed := q
	changed.Arguments = json.RawMessage(`{"text":"other"}`)
	if _, err := client.Execute(t.Context(), changed, func(context.Context) error { return nil }); core.CodeOf(err) != core.Conflict {
		t.Fatal("conflicting submit replayed another result", err)
	}
	if _, err = client.Await(t.Context(), q.Identity, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err = client.Cancel(t.Context(), q.Identity); err != nil {
		t.Fatal(err)
	}
	if err = client.Revoke(t.Context(), q.Identity.OwnerID, 1); err != nil {
		t.Fatal(err)
	}
	bad, err := NewClient(server.URL, strings.Repeat("0", 64), token, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = bad.Info(t.Context()); err == nil {
		t.Fatal("wrong fingerprint accepted")
	}
	bad, err = NewClient(server.URL, hex.EncodeToString(sum[:]), core.NewSecret(strings.Repeat("b", 48)), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = bad.Info(t.Context()); err == nil {
		t.Fatal("wrong service token accepted")
	}
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/v2/sandbox/info", nil)
	req.Header.Set("Authorization", "Bearer "+token.Reveal())
	req.Header.Set("Origin", "https://attacker.example")
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("browser origin accepted")
	}
}

func TestReadEndpointCannotMutate(t *testing.T) {
	m := managerFixture(t, &testBackend{})
	q := testRequest()
	if _, err := m.Read(t.Context(), q); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("read endpoint allowed mutation", err)
	}
	q.Method = "download"
	if _, err := m.Read(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(t.Context(), q.Identity); !persistence.Missing(err) {
		t.Fatal("transient blob persisted in operation ledger")
	}
}

func TestOldSandboxProtocolRejectedBeforeExecution(t *testing.T) {
	var submitted atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/info") {
			submitted.Add(1)
		}
		data, _ := json.Marshal(Info{Protocol: 1, Enabled: true})
		json.NewEncoder(w).Encode(response{Data: data})
	}))
	defer server.Close()
	sum := sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)
	client, err := NewClient(server.URL, hex.EncodeToString(sum[:]), core.NewSecret(strings.Repeat("a", 48)), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Execute(t.Context(), testRequest(), func(context.Context) error { return nil })
	if core.ReasonOf(err) != "sandbox_protocol_unsupported" || submitted.Load() != 0 {
		t.Fatalf("old backend reached: %v submitted=%d", err, submitted.Load())
	}
}

func TestQuotaControlsBindExecutionIdentityAndServiceCredential(t *testing.T) {
	m := managerFixture(t, &testBackend{})
	token := core.NewSecret(strings.Repeat("quota-secret", 4))
	handler, err := Handler(m, token)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(handler)
	defer server.Close()
	sum := sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)
	client, err := NewClient(server.URL, hex.EncodeToString(sum[:]), token, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	q := testRequest()
	if _, err = client.Execute(t.Context(), q, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err = client.UpdateQuotas(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	foreign := q
	foreign.Identity.AssistantID = "another"
	if err = client.UpdateQuotas(t.Context(), foreign); err == nil {
		t.Fatal("foreign execution quota update accepted")
	}
	q.Method = "quota_usage"
	if _, err = client.QuotaUsage(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	bad, err := NewClient(server.URL, hex.EncodeToString(sum[:]), core.NewSecret(strings.Repeat("wrong", 10)), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = bad.QuotaUsage(t.Context(), q); err == nil {
		t.Fatal("unauthenticated usage read")
	}
}
