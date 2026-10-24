package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"molr.ai/runtime/internal/agent/prompt"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	sm "molr.ai/runtime/internal/sandboxmanager"
)

func assertNetworkScope(t *testing.T, raw json.RawMessage, id core.ID, expected config.SandboxNetwork, granted, known bool) {
	t.Helper()
	var listing resourceListing
	need(t, json.Unmarshal(raw, &listing))
	foundRequest, foundGrant := false, false
	check := func(n *networkPermissionScope) {
		t.Helper()
		if n == nil || n.Known != known {
			t.Fatalf("missing or misleading scope: %+v", n)
		}
		if known {
			if n.Target == nil || !reflect.DeepEqual(*n.Target, expected.Target()) {
				t.Fatalf("scope differs from actual deployment: %+v", n)
			}
			if len(n.Restrictions) == 0 || !strings.Contains(n.LoopbackAccess, "host.molr.internal") {
				t.Fatalf("missing network restrictions: %+v", n)
			}
		} else if n.Target != nil || n.Reason == "" {
			t.Fatal("offline/missing scope advertised as known")
		}
	}
	for _, r := range listing.Requestable {
		if r.ResourceID == id {
			foundRequest = true
			check(r.Network)
		}
	}
	for _, r := range listing.Resources {
		if r.ResourceID == id {
			foundGrant = true
			check(r.Network)
		}
	}
	if !foundRequest || foundGrant != granted {
		t.Fatal("requestable/granted scopes mixed")
	}
	// Check the final model-facing projection, not just the internal JSON DTO.
	text, err := prompt.Structured("permission_list", string(raw))
	need(t, err)
	if !strings.Contains(text, `"Known":`) || !strings.Contains(text, "Restrictions") || !strings.Contains(text, "supports only TCP/UDP") || !strings.Contains(text, "such as ICMP, are not forwarded") {
		t.Fatal("scope stripped from model prompt")
	}
	if known {
		for _, v := range []string{"Protocols", "Addresses", "Ports", "ExcludeAddresses", "ExcludePorts", expected.Ports[0]} {
			if !strings.Contains(text, v) {
				t.Fatalf("model lost %s", v)
			}
		}
	}
}

func TestPermissionListReportsActualNetworkScopeBeforeAndAfterGrant(t *testing.T) {
	if platform.NativeSandbox() {
		t.Skip("requires Linux destination network rules; macOS uses a network toggle")
	}
	target := config.SandboxNetwork{Name: "narrow", Protocols: []string{"tcp"}, Addresses: []string{"192.168.1.0/24"}, Ports: []string{"80", "1083"}, ExcludeAddresses: []string{"192.168.1.9"}, ExcludePorts: []string{"80"}}
	f := newResourceAccessFixture(t, func(c *config.Config, _ string) { c.Sandbox.Networks = []config.SandboxNetwork{target} })
	network, err := f.b.Services.Resources.Connect(t.Context(), f.owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "network"}, Kind: resource.Network, Provider: "sandbox-network", Name: "All networks (misleading name)", Binding: resource.Binding{SandboxTarget: target.Name}})
	need(t, err)
	router, err := newSandboxRouter(f.ad, f.b.sandbox, make([]byte, 32))
	need(t, err)
	raw, err := router.list(t.Context(), f.execution.Principal)
	need(t, err)
	assertNetworkScope(t, raw, network.ID, target, false, true)
	g := f.grant(t, network.ID, "grant-network", []policy.Action{policy.Execute}, policy.Scope{}, nil)
	raw, err = router.list(t.Context(), f.execution.Principal)
	need(t, err)
	assertNetworkScope(t, raw, network.ID, target, true, true)
	router.adapters.storageConfig.Networks = nil
	raw, err = router.list(t.Context(), f.execution.Principal)
	need(t, err)
	assertNetworkScope(t, raw, network.ID, target, true, false)
	need(t, f.b.Services.Grants.Revoke(t.Context(), f.owner, policy.RevokeRequest{GrantID: g.ID, Mutation: core.Mutation{IdempotencyKey: "revoke-network", ExpectedVersion: g.Version}}))
	raw, err = router.list(t.Context(), f.execution.Principal)
	need(t, err)
	assertNetworkScope(t, raw, network.ID, target, false, false)
}

func TestPermissionListUsesLiveRemoteNetworkRulesAndMarksOfflineUnknown(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires an in-process Linux remote server; covered by Linux CI")
	}
	f := newResourceAccessFixture(t, nil)
	cfg := testConfig(t)
	target := config.SandboxNetwork{Name: "remote-only", Protocols: []string{"udp"}, Addresses: []string{"10.99.0.0/16"}, Ports: []string{"1083-1085"}, ExcludeAddresses: []string{"10.99.0.2"}, ExcludePorts: []string{"1084"}}
	cfg.Sandbox.Networks = []config.SandboxNetwork{target}
	backend, err := newSandboxBackend(t.Context(), cfg, quotaTestStore(t))
	need(t, err)
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "remote.db"))
	need(t, err)
	t.Cleanup(func() { store.Close() })
	manager, err := sm.New(t.Context(), store, backend)
	need(t, err)
	t.Cleanup(func() { manager.Close() })
	token := strings.Repeat("private-token-", 8)
	handler, err := sm.Handler(manager, core.NewSecret(token))
	need(t, err)
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	digest := sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)
	f.ad.networkConfig.AllowedPrivateIPs = []string{"127.0.0.1"}
	router, err := newSandboxRouter(f.ad, f.b.sandbox, make([]byte, 32))
	need(t, err)
	admin := func(action string, value any) any {
		t.Helper()
		raw, _ := json.Marshal(value)
		out, err := router.admin(t.Context(), f.owner, action, raw)
		need(t, err)
		return out
	}
	connection := admin("connect", map[string]any{"Name": "Remote", "Endpoint": server.URL, "ConnectionCode": sandboxConnectionCode(hex.EncodeToString(digest[:]), token), "Mutation": core.Mutation{IdempotencyKey: "remote"}}).(sandboxView)
	network := admin("resource", map[string]any{"SandboxID": connection.ID, "Kind": "network", "SourceID": target.Name, "Name": "LAN", "Mutation": core.Mutation{IdempotencyKey: "remote-network"}}).(resource.Resource)
	raw, err := router.list(t.Context(), f.execution.Principal)
	need(t, err)
	assertNetworkScope(t, raw, network.ID, target, false, true)
	f.grant(t, network.ID, "remote-grant", []policy.Action{policy.Execute}, policy.Scope{}, nil)
	raw, err = router.list(t.Context(), f.execution.Principal)
	need(t, err)
	assertNetworkScope(t, raw, network.ID, target, true, true)
	if strings.Contains(string(raw), token) || strings.Contains(string(raw), server.URL) || strings.Contains(string(raw), cfg.DataDir) {
		t.Fatal("private remote binding leaked")
	}
	server.Close()
	raw, err = router.list(t.Context(), f.execution.Principal)
	need(t, err)
	assertNetworkScope(t, raw, network.ID, target, true, false)
	// Other owners' network resources must never enter this assistant's catalog.
	need(t, f.b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.resources", persistence.Key("other", "hidden"), resource.Resource{ID: "hidden", OwnerID: "other", Kind: resource.Network, Name: "private-other-network", State: resource.Available, Actions: []string{"execute"}})
	}))
	raw, err = router.list(t.Context(), f.execution.Principal)
	need(t, err)
	if strings.Contains(string(raw), "private-other-network") {
		t.Fatal("cross-owner network revealed")
	}
}
