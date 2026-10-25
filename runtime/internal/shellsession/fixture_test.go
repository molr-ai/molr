package shellsession

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/portforward"
	"path/filepath"
	"testing"
)

type sessionFixture struct {
	store      persistence.Store
	shellTasks *Manager
	Services   struct{ Auth auth.Service }
}

func need(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func taskFixture(t *testing.T) (*sessionFixture, core.Principal, core.Execution) {
	t.Helper()
	root := t.TempDir()
	store, err := sqlitestore.Open(filepath.Join(root, "test.db"))
	need(t, err)
	t.Cleanup(func() { store.Close() })
	key := make([]byte, 32)
	identity, err := auth.NewService(store, auth.Options{MasterKey: key, Origin: "https://runtime.example"})
	need(t, err)
	_, err = identity.Initialize(t.Context(), auth.InitializeRequest{Username: "owner", Password: core.NewSecret("correct-horse-battery-staple-2026!")})
	need(t, err)
	login, err := identity.Login(t.Context(), auth.LoginRequest{Username: "owner", Password: core.NewSecret("correct-horse-battery-staple-2026!"), Client: auth.Client{Address: "127.0.0.1", DeviceName: "test"}})
	need(t, err)
	p, err := identity.Authenticate(t.Context(), auth.Authentication{Session: login.Session.Token})
	need(t, err)
	block, err := aes.NewCipher(key)
	need(t, err)
	box, err := cipher.NewGCM(block)
	need(t, err)
	host := fixtureHost{store: store, root: root, identity: identity, box: box}
	cfg := config.Defaults(root)
	s := New(store, host, Options{Identity: identity, Forward: cfg.PortForward, ForwardHTTP: cfg.HTTP, Browser: cfg.Browser})
	t.Cleanup(s.Close)
	b := &sessionFixture{store: store, shellTasks: s}
	b.Services.Auth = identity
	need(t, resource.NewRepository(store).Insert(t.Context(), resource.Resource{ID: "compute", OwnerID: p.OwnerID, Kind: resource.Compute, Provider: "managed-compute", State: resource.Available, Version: 1}, resource.Binding{ResourceID: "compute"}))
	e := core.Execution{Principal: core.Principal{OwnerID: p.OwnerID, SubjectID: "shell-assistant", Kind: core.ActorAssistant, ExecutionID: "shell-run"}, AssistantID: "shell-assistant", OperationID: "shell-operation"}
	need(t, store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "business.assistants", persistence.Key(p.OwnerID, e.AssistantID), assistant.Assistant{ID: e.AssistantID, OwnerID: p.OwnerID, State: assistant.Active, Version: 1}); err != nil {
			return err
		}
		return tx.Put(ctx, "agent.jobs", persistence.Key(p.OwnerID, "shell-job"), scheduler.Job{State: scheduler.Running, ExecutionID: e.Principal.ExecutionID, Event: event.AssistantEvent{ID: "shell-job", OwnerID: p.OwnerID, AssistantID: e.AssistantID}})
	}))
	return b, p, e
}

// This host models the session boundary. Real routing, grant and account
// revocation remain covered by the app composition/integration suites.
type fixtureHost struct {
	store    persistence.Store
	root     string
	identity *auth.Manager
	box      cipher.AEAD
}

func (h fixtureHost) Execute(context.Context, core.Execution, tools.ShellRequest) (json.RawMessage, error) {
	return nil, core.NewError(core.Unavailable, "test executor not configured")
}
func (h fixtureHost) CheckAssistant(ctx context.Context, p core.Principal, id core.ID) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.Kind == core.ActorOwner {
		if err := h.identity.ValidatePrincipal(ctx, p); err != nil {
			return err
		}
	}
	if p.Kind == core.ActorAssistant && p.SubjectID != id {
		return denied()
	}
	a, err := assistant.NewRepository(h.store).Get(ctx, p.OwnerID, id)
	if err != nil {
		return err
	}
	if a.State != assistant.Active {
		return denied()
	}
	return nil
}
func (h fixtureHost) CheckExecution(ctx context.Context, e core.Execution) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if err := h.CheckAssistant(ctx, e.Principal, e.AssistantID); err != nil {
		return err
	}
	if ManagedFor(ctx, e) != nil {
		return nil
	}
	rows, err := persistence.Select[scheduler.Job](ctx, h.store, "agent.jobs", persistence.Query{Where: []persistence.Filter{persistence.Eq("Event.OwnerID", e.Principal.OwnerID), persistence.Eq("Event.AssistantID", e.AssistantID), persistence.Eq("ExecutionID", e.Principal.ExecutionID), persistence.Eq("State", scheduler.Running)}, Limit: 1})
	if err != nil {
		return err
	}
	if len(rows) != 1 {
		return denied()
	}
	return nil
}
func (h fixtureHost) AuthorizeForward(ctx context.Context, p core.Principal) error {
	return h.identity.RequireAdministrator(ctx, p)
}
func (h fixtureHost) ResolveCompute(ctx context.Context, p core.Principal, id, node core.ID) (core.ID, error) {
	r, err := resource.NewRepository(h.store).Get(ctx, p.OwnerID, id)
	if err != nil {
		return "", err
	}
	if node == "" {
		node = "local"
	}
	if r.SandboxID != "" && r.SandboxID != node {
		return "", denied()
	}
	return node, nil
}
func (h fixtureHost) ConcurrencyLimit(context.Context, core.ID, core.ID) (int, error) { return 0, nil }
func (h fixtureHost) DialForward(context.Context, core.Execution, core.ID, int) (portforward.Stream, error) {
	return nil, core.NewError(core.Unavailable, "no test forward")
}
func (h fixtureHost) CaptureLogs(context.Context, core.Principal, core.ID, core.ID) error { return nil }
func (h fixtureHost) Workspace(owner, id core.ID) string {
	return filepath.Join(h.root, "work", string(owner), string(id))
}
func (h fixtureHost) BrowserSandboxAvailable() bool { return false }
func (h fixtureHost) BrowserSupported(bool) bool    { return false }
func (h fixtureHost) AuthorizeSession(ctx context.Context, e core.Execution) error {
	return h.CheckExecution(ctx, e)
}
func (h fixtureHost) RecordAudit(ctx context.Context, r audit.Record) error {
	return audit.NewRepository(h.store).Append(ctx, r)
}
func (h fixtureHost) ProxyCipher() cipher.AEAD { return h.box }
func (h fixtureHost) VerifyProxy(context.Context, core.Principal, auth.Verification) error {
	return nil
}
