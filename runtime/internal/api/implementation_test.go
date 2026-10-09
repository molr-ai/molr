package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/dream"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/business/plan"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/debugtrace"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/pricing"
	"molr.ai/runtime/internal/search"
	"molr.ai/runtime/internal/storage/files"
	"molr.ai/runtime/tests/testkit"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureCertificate(t *testing.T, dir string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	testkit.RequireNoError(t, err)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "molr.example"}, DNSNames: []string{"molr.example"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	testkit.RequireNoError(t, err)
	private, err := x509.MarshalPKCS8PrivateKey(key)
	testkit.RequireNoError(t, err)
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	testkit.RequireNoError(t, os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600))
	testkit.RequireNoError(t, os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600))
	return certPath, keyPath
}
func init() {
	newContractFixture = func(t *testing.T) contractFixture {
		dir := t.TempDir()
		store, err := sqlitestore.Open(filepath.Join(dir, "api.db"))
		testkit.RequireNoError(t, err)
		t.Cleanup(func() { _ = store.Close() })
		ctx := testkit.Context(t)
		now := time.Now().UTC()
		var mu sync.Mutex
		clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
		identity, err := auth.NewService(store, auth.Options{MasterKey: make([]byte, 32), Origin: "https://molr.example", Now: clock})
		testkit.RequireNoError(t, err)
		_, err = identity.Initialize(ctx, auth.InitializeRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!")})
		testkit.RequireNoError(t, err)
		login, err := identity.Login(ctx, auth.LoginRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!")})
		testkit.RequireNoError(t, err)
		owner, err := identity.Authenticate(ctx, auth.Authentication{Session: login.Session.Token})
		testkit.RequireNoError(t, err)
		// Ownership is a controlled composition dependency; all module services and
		// HTTP/auth/CSRF/limiter/stream persistence are their real implementations.
		permissions, err := policy.NewService(store, policy.Options{Now: clock, ValidatePrincipal: identity.ValidatePrincipal, CheckScope: identity.CheckScope, Owns: func(ctx context.Context, p core.Principal, o core.Object) error {
			if p != owner {
				return core.NewError(core.Forbidden, "fixture owner only")
			}
			return nil
		}})
		testkit.RequireNoError(t, err)
		auditService, err := audit.NewService(store, audit.Options{ValidatePrincipal: identity.ValidatePrincipal, CheckScope: identity.CheckScope})
		testkit.RequireNoError(t, err)
		vault, err := credential.NewService(store, credential.Options{MasterKey: make([]byte, 32), Now: clock, ValidatePrincipal: identity.ValidatePrincipal, CheckScope: identity.CheckScope, CheckVerification: identity.CheckVerification})
		testkit.RequireNoError(t, err)
		objects, err := files.NewObjects(filepath.Join(dir, "files"), store)
		testkit.RequireNoError(t, err)
		t.Cleanup(func() { _ = objects.Close() })
		var calls atomic.Int64
		models := model.NewClient(store, model.Options{Authorizer: permissions, ListModels: func(context.Context, core.Principal) ([]model.Model, error) {
			calls.Add(1)
			return []model.Model{{ID: "fixture-model", Name: "Fixture model", ResourceID: "fixture-resource", ContextLimit: 1000}}, nil
		}})
		exchange, err := pricing.NewExchange(nil)
		testkit.RequireNoError(t, err)
		services := Services{Dreams: dream.New(store, dream.Options{Authorizer: permissions}), Plans: plan.New(store, permissions, task.NewService(store, task.Options{Authorizer: permissions})), Debug: debugtrace.New(store, debugtrace.Options{}), Exchange: exchange, Auth: identity, Browser: identity, Audit: auditService,
			Assistants: assistant.NewService(store, assistant.Options{Authorizer: permissions}), Conversations: conversation.NewService(store, conversation.Options{Authorizer: permissions, Visible: realtime.NewStore(store)}), Tasks: task.NewService(store, task.Options{Authorizer: permissions}), Todos: todo.NewService(store, todo.Options{Authorizer: permissions}), Schedules: schedule.NewService(store, schedule.Options{Authorizer: permissions}), Resources: resource.NewService(store, resource.Options{Authorizer: permissions}), Memories: memory.NewService(store, memory.Options{Authorizer: permissions}), Settings: settings.NewService(store, settings.Options{Authorizer: permissions, CheckVerification: identity.CheckVerification}), Models: models, Credentials: vault, Grants: permissions, Budget: permissions, Agent: agent.NewRuntime(store, agent.Options{Dependencies: agent.Dependencies{Policy: permissions, Budget: permissions, Models: models}}), Jobs: scheduler.NewService(store, scheduler.Options{Authorizer: permissions}), Operations: operation.NewService(store, operation.Options{Authorizer: permissions}), Search: search.NewService(store, search.Options{Authorizer: permissions}), Index: search.NewIndexer(store, search.Options{Authorizer: permissions}), Files: files.NewService(store, objects, files.Options{Authorizer: permissions, MaxUploadBytes: 1 << 20, DiskQuota: func() int64 { return 10 << 20 }}), Realtime: realtime.NewFeed(realtime.NewStore(store), realtime.Options{Authorizer: permissions, WatchAuthorization: func(p core.Principal) (realtime.AuthorizationWatch, error) { return identity.WatchPrincipal(p) }})}
		limiter, err := NewRateLimiter(store, RateLimitOptions{Now: clock, Limit: 120, CategoryLimits: map[string]int{"login": 2}})
		testkit.RequireNoError(t, err)
		cfg := config.Defaults(dir)
		cfg.HTTP.PublicURL = "https://molr.example"
		cfg.HTTP.CertFile, cfg.HTTP.KeyFile = fixtureCertificate(t, dir)
		ready := make(chan struct{})
		server, err := NewServer(cfg, services, Options{Notifications: notification.New(store, permissions), Limiter: limiter, PreLoginCSRF: identity.PreLoginCSRF, Listen: func(string, string) (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") }, OnListen: func(net.Addr) { close(ready) }})
		testkit.RequireNoError(t, err)
		t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
		request := func(method, path, body string) *http.Request {
			r := httptest.NewRequest(method, "https://molr.example"+path, strings.NewReader(body))
			r.AddCookie(&http.Cookie{Name: sessionCookie, Value: login.Session.Token.Reveal()})
			r.Header.Set("Origin", "https://molr.example")
			r.Header.Set("X-CSRF-Token", login.Session.CSRF.Reveal())
			if body != "" {
				r.Header.Set("Content-Type", "application/json")
			}
			return r
		}
		return contractFixture{SeedNotification: func() core.ID {
			testkit.RequireNoError(t, notification.Sync(ctx, store, owner.OwnerID, notification.Input{Kind: "weixin.delivery", Code: "unknown:unknown", Source: core.Object{Kind: "weixin_delivery", ID: "test-delivery"}}, true))
			page, err := notification.New(store, permissions).List(ctx, owner, notification.Query{})
			testkit.RequireNoError(t, err)
			return page.Items[0].ID
		}, SeedDreamAssistant: func() core.ID {
			id := core.ID("dream-assistant")
			testkit.RequireNoError(t, store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				return tx.Put(ctx, "business.assistants", persistence.Key(owner.OwnerID, id), assistant.Assistant{ID: id, OwnerID: owner.OwnerID, Name: "Dream", State: assistant.Active, Version: 1})
			}))
			return id
		}, Server: server, Services: services, Limiter: limiter, ReadRequest: func() *http.Request { return request("GET", "/api/models", "") }, MutationRequest: func() *http.Request {
			return request("POST", "/api/assistants", `{"Mutation":{"IdempotencyKey":"hire"},"Name":"Assistant","ModelID":"fixture-model"}`)
		}, SSERequest: func() *http.Request { return request("GET", "/api/events/stream", "") }, StripCredentials: func(r *http.Request) { r.Header.Del("Cookie"); r.Header.Del("Authorization") }, StripCSRF: func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, RevokeSession: func() { testkit.RequireNoError(t, identity.RevokeSession(ctx, owner, owner.SessionID)) }, ServiceCalls: func() int { return int(calls.Load()) }, Advance: func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }, FailLimiter: func() { testkit.RequireNoError(t, store.Close()) }, Started: ready}
	}
}
