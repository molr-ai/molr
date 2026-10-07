package sandboxmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
)

func TestForwardEndpointRequiresServiceAuthentication(t *testing.T) {
	m := managerFixture(t, &testBackend{})
	token := strings.Repeat("forward-secret", 4)
	handler, err := Handler(m, core.NewSecret(token))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ token, origin string }{{"", ""}, {"wrong", ""}, {token, "https://browser.example"}} {
		req := httptest.NewRequest(http.MethodPost, "https://sandbox/v2/sandbox/forward", bytes.NewBufferString(`{}`))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Upgrade", "molr-tcp-v1")
		req.Header.Set("Connection", "Upgrade")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("unexpected forward authentication status %d", res.Code)
		}
	}
}
func TestForwardRequiresLiveExactIdentityAndLease(t *testing.T) {
	b := &testBackend{release: make(chan struct{})}
	m := managerFixture(t, b)
	q := testRequest()
	q.Method = "shell_execute"
	q.Arguments = json.RawMessage(`{}`)
	if _, err := m.Submit(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	foreign := q.Identity
	foreign.AssistantID = "another"
	for _, id := range []Identity{foreign, {OwnerID: "other", AssistantID: q.Identity.AssistantID, ExecutionID: q.Identity.ExecutionID, OperationID: q.Identity.OperationID}} {
		if c, err := m.dialForward(t.Context(), forwardRequest{Identity: id, Port: 80}); err == nil {
			c.Close()
			t.Fatal("foreign identity allowed")
		}
	}
	m.mu.Lock()
	m.active[operationKey(q.Identity)].lease = time.Now().Add(-time.Second)
	m.mu.Unlock()
	if c, err := m.dialForward(context.Background(), forwardRequest{Identity: q.Identity, Port: 80}); core.CodeOf(err) != core.Forbidden {
		if c != nil {
			c.Close()
		}
		t.Fatalf("expired lease: %v", err)
	}
	close(b.release)
}
