package app

import (
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/net/websocket"
	"io"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNativeSandboxGrantRoutingAndRevocation(t *testing.T) {
	b, owner, e := taskFixture(t)
	ctx := t.Context()
	need(t, b.sandbox.runner.Prepare(ctx))
	compute, err := b.Services.Resources.Connect(ctx, owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "native-compute"}, Kind: resource.Compute, Provider: "local-sandbox", Name: "Compute"})
	need(t, err)
	network, err := b.Services.Resources.Connect(ctx, owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "native-network"}, Kind: resource.Network, Provider: "sandbox-network", Name: "Network access", Binding: resource.Binding{SandboxTarget: "preset:network"}})
	need(t, err)
	duration := &policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 30}}
	grant := func(id core.ID, key string, c *policy.Constraints) policy.Grant {
		t.Helper()
		g, err := b.Services.Grants.Grant(ctx, owner, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: key}, AssistantID: e.AssistantID, ResourceID: id, Actions: []policy.Action{policy.Execute}, Constraints: c})
		need(t, err)
		return g
	}
	// Unsupported promises are rejected by the same grant API used by the UI.
	for _, q := range []struct {
		id     core.ID
		limits *policy.Constraints
	}{{compute.ID, &policy.Constraints{Compute: &policy.ComputeConstraints{CPUPercent: 100, MaxDurationSeconds: 30}}}, {network.ID, &policy.Constraints{Network: &policy.NetworkConstraints{Bytes: 1024, Connections: 1, Period: "day", Timezone: "UTC"}}}} {
		_, err := b.Services.Grants.Grant(ctx, owner, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "bad-" + string(q.id)}, AssistantID: e.AssistantID, ResourceID: q.id, Actions: []policy.Action{policy.Execute}, Constraints: q.limits})
		if err == nil {
			t.Fatal("unsupported quota accepted")
		}
	}
	request := tools.ShellRequest{ComputeID: compute.ID, Script: "echo hello"}
	if _, err := b.sandbox.plan(ctx, e, request); err == nil {
		t.Fatal("no compute grant")
	}
	grant(compute.ID, "duration", duration)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "CONNECTED") }))
	defer server.Close()
	run := func(id string, none bool, want bool) {
		t.Helper()
		e.OperationID = core.ID(id)
		q := tools.ShellRequest{ComputeID: compute.ID, Script: "curl --noproxy '*' --silent --max-time 2 " + server.URL}
		if none {
			q.NetworkMode = "none"
		}
		raw, err := b.remote.shellExecute(ctx, e, q)
		need(t, err)
		var result struct {
			Exit   platform.Exit
			Stdout string
		}
		need(t, json.Unmarshal(raw, &result))
		if (result.Exit.Code == 0) != want || strings.Contains(string(raw), "CONNECTED") != want {
			t.Fatalf("%s %s", id, raw)
		}
	}
	run("offline", false, false)
	ng := grant(network.ID, "network", nil)
	run("online", false, true)
	run("forced-offline", true, false)
	// A frozen launch scope includes selected network grants; new grants are not retroactive.
	request.NetworkIDs = []core.ID{network.ID}
	request.Script = "sleep 30"
	e.OperationID = "revoked-network"
	plan, err := b.sandbox.plan(ctx, e, request)
	need(t, err)
	if !plan.NetworkAccess || plan.ComputeQuota || len(plan.NetworkTargets) != 0 {
		t.Fatalf("%+v", plan)
	}
	b.sandbox.leases.Store(shellKey(e), shellLease{request: request, plan: plan})
	defer b.sandbox.leases.Delete(shellKey(e))
	p, err := b.sandbox.runner.Start(ctx, e, plan)
	need(t, err)
	need(t, b.Services.Grants.Revoke(ctx, owner, policy.RevokeRequest{Mutation: core.Mutation{IdempotencyKey: "revoke-network", ExpectedVersion: ng.Version}, GrantID: ng.ID}))
	deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	exit, err := p.Wait(deadline)
	need(t, err)
	if exit.Reason != "authorization_changed" {
		t.Fatalf("%+v", exit)
	}
	run("after-revoke", false, false)
	// Managed task cancellation and terminal ownership still use existing services.
	e.OperationID = "native-terminal"
	raw, err := b.shellTasks.execute(ctx, e, tools.ShellRequest{ComputeID: compute.ID, Script: "exec /bin/sh -i", TTY: true})
	need(t, err)
	if !strings.Contains(string(raw), "TTY") {
		t.Fatal(string(raw))
	}
	for deadline := time.Now().Add(3 * time.Second); ; {
		b.shellTasks.mu.Lock()
		task := b.shellTasks.active[shellKey(e)]
		ready := task != nil && task.scope.control != nil
		b.shellTasks.mu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("terminal not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, err = b.shellTasks.manage(ctx, owner, e.AssistantID, tools.ShellTaskRequest{Action: "write", TaskID: e.OperationID, Data: "echo TERMINAL_OK\n"})
	need(t, err)
	time.Sleep(100 * time.Millisecond)
	raw, err = b.shellTasks.manage(ctx, owner, e.AssistantID, tools.ShellTaskRequest{Action: "read", TaskID: e.OperationID})
	need(t, err)
	if !strings.Contains(string(raw), "TERMINAL_OK") {
		t.Fatal(string(raw))
	}
	_, err = b.shellTasks.manage(ctx, owner, e.AssistantID, tools.ShellTaskRequest{Action: "stop", TaskID: e.OperationID})
	need(t, err)
	rows, err := persistence.Select[policy.Grant](ctx, b.store, "security.grants", persistence.Query{})
	need(t, err)
	if len(rows) != 2 {
		t.Fatalf("%d grants", len(rows))
	}
}

func TestNativeSandboxAuthenticatedHTTPAndWebSocketForward(t *testing.T) {
	b, owner, e := taskFixture(t)
	ctx := t.Context()
	need(t, b.sandbox.runner.Prepare(ctx))
	s := b.shellTasks
	s.forwardHTTP.TLSMode = config.LocalHTTP
	s.forwardConfig = config.PortForward{Enabled: true, ListenHost: "127.0.0.1", PublicHost: "127.0.0.1"}
	login, err := b.Services.Auth.Login(ctx, auth.LoginRequest{Username: "owner", Password: core.NewSecret("correct-horse-battery-staple-2026!"), Client: auth.Client{Address: "127.0.0.1", DeviceName: "forward"}})
	need(t, err)
	cookie := (&http.Cookie{Name: "molr_dev_session_p8080", Value: login.Session.Token.Reveal()}).String()
	compute, err := b.Services.Resources.Connect(ctx, owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "compute"}, Kind: resource.Compute, Provider: "local-sandbox", Name: "compute"})
	need(t, err)
	network, err := b.Services.Resources.Connect(ctx, owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "network"}, Kind: resource.Network, Provider: "sandbox-network", Name: "network", Binding: resource.Binding{SandboxTarget: "preset:network"}})
	need(t, err)
	_, err = b.Services.Grants.Grant(ctx, owner, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "compute-grant"}, AssistantID: e.AssistantID, ResourceID: compute.ID, Actions: []policy.Action{policy.Execute}, Constraints: &policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 30}}})
	need(t, err)
	ng, err := b.Services.Grants.Grant(ctx, owner, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "network-grant"}, AssistantID: e.AssistantID, ResourceID: network.ID, Actions: []policy.Action{policy.Execute}})
	need(t, err)
	mux := http.NewServeMux()
	mux.Handle("/ws", websocket.Handler(func(c *websocket.Conn) { defer c.Close(); io.Copy(c, c) }))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("credential forwarded")
		}
		fmt.Fprint(w, "NATIVE_HTTP")
	})
	target := httptest.NewServer(mux)
	defer target.Close()
	_, p, _ := net.SplitHostPort(strings.TrimPrefix(target.URL, "http://"))
	port, _ := strconv.Atoi(p)
	raw, err := s.execute(ctx, e, tools.ShellRequest{ComputeID: compute.ID, Script: "sleep 30", Background: true, ForwardPorts: []int{port}})
	need(t, err)
	var receipt struct{ Task shellTask }
	need(t, json.Unmarshal(raw, &receipt))
	if len(receipt.Task.Forwards) != 1 {
		t.Fatal(string(raw))
	}
	url := receipt.Task.Forwards[0].URL
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	for deadline := time.Now().Add(5 * time.Second); ; {
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Cookie", cookie)
		req.Header.Set("Authorization", "Bearer synthetic")
		res, err := client.Do(req)
		if err == nil {
			body, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if res.StatusCode == 200 && string(body) == "NATIVE_HTTP" {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("forward not ready: %v %s", err, raw)
		}
		time.Sleep(25 * time.Millisecond)
	}
	anon, err := client.Get(url)
	need(t, err)
	anon.Body.Close()
	if anon.StatusCode != 401 {
		t.Fatalf("anonymous: %d", anon.StatusCode)
	}
	wsConfig, err := websocket.NewConfig(strings.Replace(url, "http", "ws", 1)+"/ws", url)
	need(t, err)
	wsConfig.Header = http.Header{"Cookie": {cookie}}
	ws, err := websocket.DialConfig(wsConfig)
	need(t, err)
	defer ws.Close()
	ws.SetDeadline(time.Now().Add(3 * time.Second))
	need(t, websocket.Message.Send(ws, "native-frame"))
	var echo string
	need(t, websocket.Message.Receive(ws, &echo))
	if echo != "native-frame" {
		t.Fatal(echo)
	}
	need(t, b.Services.Grants.Revoke(ctx, owner, policy.RevokeRequest{Mutation: core.Mutation{IdempotencyKey: "revoke", ExpectedVersion: ng.Version}, GrantID: ng.ID}))
	ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err = websocket.Message.Receive(ws, &echo); err == nil {
		t.Fatal("revoked websocket remains open")
	}
	for deadline := time.Now().Add(3 * time.Second); ; {
		res, err := client.Get(url)
		if err != nil {
			break
		}
		res.Body.Close()
		if time.Now().After(deadline) {
			t.Fatal("forward listener survived revocation")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestNativeNetworkPermissionScopeTracksGrantAndDeployment(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	network, err := f.b.Services.Resources.Connect(t.Context(), f.owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "network"}, Kind: resource.Network, Provider: "sandbox-network", Name: "Network", Binding: resource.Binding{SandboxTarget: "preset:network"}})
	need(t, err)
	check := func(granted, known bool) {
		t.Helper()
		listing := f.list(t)
		checkScope := func(scope *networkPermissionScope) {
			t.Helper()
			if scope == nil || scope.Mode != "all_ip" || scope.Known != known || !strings.Contains(scope.LoopbackAccess, "host") {
				t.Fatalf("incorrect native scope: %+v", scope)
			}
			if known && (scope.Target == nil || scope.Reason != "") {
				t.Fatal("available toggle missing scope", scope)
			}
			if !known && (scope.Target != nil || scope.Reason == "") {
				t.Fatal("missing toggle advertised as available", scope)
			}
		}
		foundRequest, foundGrant := false, false
		for _, r := range listing.Requestable {
			if r.ResourceID == network.ID {
				foundRequest = true
				checkScope(r.Network)
			}
		}
		for _, r := range listing.Resources {
			if r.ResourceID == network.ID {
				foundGrant = true
				checkScope(r.Network)
			}
		}
		if !foundRequest || foundGrant != granted {
			t.Fatal("network discovery confused with authorization")
		}
	}
	check(false, true)
	grant := f.grant(t, network.ID, "grant", []policy.Action{policy.Execute}, policy.Scope{}, nil)
	if grant.Constraints != nil {
		t.Fatal("native network toggle acquired hard quotas")
	}
	check(true, true)
	f.catalog.adapters.storageConfig.NetworkPresets = []string{}
	check(true, false)
	need(t, f.b.Services.Grants.Revoke(t.Context(), f.owner, policy.RevokeRequest{GrantID: grant.ID, Mutation: core.Mutation{IdempotencyKey: "revoke", ExpectedVersion: grant.Version}}))
	check(false, false)
}
