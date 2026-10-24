package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	sm "molr.ai/runtime/internal/sandboxmanager"
)

// Runs natively on macOS, or from Linux against a dedicated native test node.
// External node credentials are supplied through a private JSON file, never fixtures.
func TestRemoteNativeSandboxIntegration(t *testing.T) {
	var target struct{ Endpoint, PublicKeySHA256, Token, NetworkURL string }
	if path := os.Getenv("MOLR_TEST_NATIVE_NODE"); path != "" {
		raw, err := os.ReadFile(path)
		need(t, err)
		need(t, json.Unmarshal(raw, &target))
	} else {
		if runtime.GOOS != "darwin" {
			t.Skip("native macOS or MOLR_TEST_NATIVE_NODE required")
		}
		cfg := testConfig(t)
		cfg.Sandbox.AllowFullAccess = true
		cfg.Sandbox.Directories = []config.SandboxDirectory{{Path: t.TempDir()}}
		backend, err := newSandboxBackend(t.Context(), cfg, quotaTestStore(t))
		need(t, err)
		manager, err := sm.New(t.Context(), quotaTestStore(t), backend)
		need(t, err)
		t.Cleanup(func() { manager.Close() })
		target.Token = strings.Repeat("native-test-", 8)
		handler, err := sm.Handler(manager, core.NewSecret(target.Token))
		need(t, err)
		server := httptest.NewTLSServer(handler)
		t.Cleanup(server.Close)
		fingerprint := sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)
		target.Endpoint, target.PublicKeySHA256 = server.URL, hex.EncodeToString(fingerprint[:])
		network := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "NETWORK_OK") }))
		t.Cleanup(network.Close)
		target.NetworkURL = network.URL
	}
	b, owner, e := taskFixture(t)
	router := b.remote
	endpoint, err := url.Parse(target.Endpoint)
	need(t, err)
	router.adapters.networkConfig.AllowedPrivateIPs = append(router.adapters.networkConfig.AllowedPrivateIPs, endpoint.Hostname())
	admin := func(action string, q any) any {
		t.Helper()
		raw, err := json.Marshal(q)
		need(t, err)
		v, err := router.admin(t.Context(), owner, action, raw)
		need(t, err)
		return v
	}
	view := admin("connect", map[string]any{"Name": "Native remote", "Endpoint": target.Endpoint, "ConnectionCode": sandboxConnectionCode(target.PublicKeySHA256, target.Token), "Mutation": core.Mutation{IdempotencyKey: "native-connect"}}).(sandboxView)
	caps := view.Info.Capabilities
	if caps.OS != "darwin" || caps.Reason != "available" || !caps.Shell || !caps.FileIsolation || !caps.NetworkIsolation || caps.ProcessIsolation || caps.ResourceLimits || !view.Info.Storage {
		t.Fatalf("unexpected capabilities: %+v", view.Info)
	}
	if len(view.Info.Directories) == 0 {
		t.Fatal("dedicated test directory required")
	}
	resourceFor := func(kind, source string) core.ID {
		return admin("resource", map[string]any{"SandboxID": view.ID, "Kind": kind, "SourceID": source, "Name": kind, "Mutation": core.Mutation{IdempotencyKey: "native-" + kind}}).(resource.Resource).ID
	}
	compute := resourceFor("compute", "compute")
	storage := resourceFor("storage", view.Info.Directories[0].ID)
	network := resourceFor("network", "preset:network")
	grant := func(id core.ID, key string, actions []policy.Action, scope policy.Scope, limits *policy.Constraints) policy.Grant {
		t.Helper()
		g, err := b.Services.Grants.Grant(t.Context(), owner, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: key}, AssistantID: e.AssistantID, ResourceID: id, Actions: actions, Scope: scope, Constraints: limits})
		need(t, err)
		return g
	}
	grant(compute, "compute", []policy.Action{policy.Execute}, policy.Scope{}, &policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 60}})
	grant(storage, "files", []policy.Action{policy.Read, policy.Write}, policy.Scope{PathPrefixes: []string{"/"}}, nil)
	// All foreground calls use the actual routing, leases and remote output archive.
	execute := func(key, script, mode string) (platform.Exit, string) {
		t.Helper()
		e.OperationID = core.ID(key)
		raw, err := router.shellExecute(t.Context(), e, tools.ShellRequest{SandboxID: view.ID, ComputeID: compute, Script: script, NetworkMode: mode})
		need(t, err)
		var result struct {
			Exit   platform.Exit
			Stdout string
		}
		need(t, json.Unmarshal(raw, &result))
		return result.Exit, result.Stdout
	}
	exit, out := execute("native-offline", "curl --noproxy '*' --silent --max-time 2 "+target.NetworkURL, "")
	if exit.Code == 0 || strings.Contains(out, "NETWORK_OK") {
		t.Fatal("network without grant", exit, out)
	}
	ng := grant(network, "network", []policy.Action{policy.Execute}, policy.Scope{}, nil)
	exit, out = execute("native-online", "curl --noproxy '*' --silent --max-time 2 "+target.NetworkURL, "")
	if exit.Code != 0 || !strings.Contains(out, "NETWORK_OK") {
		t.Fatal("authorized network", exit, out)
	}
	exit, out = execute("native-forced-offline", "curl --noproxy '*' --silent --max-time 2 "+target.NetworkURL, "none")
	if exit.Code == 0 {
		t.Fatal("network_mode:none ignored", out)
	}
	raw, err := router.list(t.Context(), e.Principal)
	need(t, err)
	var listing resourceListing
	need(t, json.Unmarshal(raw, &listing))
	variable := ""
	for _, r := range listing.Resources {
		if r.ResourceID == storage {
			variable = r.ShellEnvironment
			if r.ShellPath != "" {
				t.Fatal("remote native mount path advertised")
			}
		}
	}
	if variable == "" {
		t.Fatal("missing resource environment")
	}
	e.OperationID = "native-resource"
	request := tools.ShellRequest{SandboxID: view.ID, ComputeID: compute, Script: `test -z "$MOLR_WORKSPACE" && test -z "$MOLR_GLOBAL" || exit 1; echo FILE_OK > "$` + variable + `/native-result"; cat "$` + variable + `/native-result"`}
	request.Files = append(request.Files, tools.ShellFile{ResourceID: storage, Path: "/", Write: true})
	raw, err = router.shellExecute(t.Context(), e, request)
	need(t, err)
	if !strings.Contains(string(raw), "FILE_OK") {
		t.Fatalf("resource variable failed: %s", raw)
	}
	e.OperationID = "native-read"
	raw, _, err = router.file(t.Context(), e, "file_read", tools.FileTarget{SandboxID: view.ID, ResourceID: storage, Path: "/native-result"}, tools.FileReadRequest{FileTarget: tools.FileTarget{SandboxID: view.ID, ResourceID: storage, Path: "/native-result"}}, "")
	need(t, err)
	if !strings.Contains(string(raw), "FILE_OK") {
		t.Fatal(string(raw))
	}
	// A TTY stays alive through renewals, then a network revoke must stop it.
	e.OperationID = "native-tty"
	_, err = b.shellTasks.execute(t.Context(), e, tools.ShellRequest{SandboxID: view.ID, ComputeID: compute, TTY: true, Script: "echo READY; read line; echo RECEIVED:$line; sleep 60"})
	need(t, err)
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, err = b.shellTasks.manage(t.Context(), owner, e.AssistantID, tools.ShellTaskRequest{Action: "takeover", TaskID: e.OperationID, ClientID: "remote-native-terminal", Generation: 1})
		if err == nil {
			break
		}
		if core.CodeOf(err) != core.FailedPrecondition || time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	_, err = b.shellTasks.manage(t.Context(), owner, e.AssistantID, tools.ShellTaskRequest{Action: "write", TaskID: e.OperationID, ClientID: "remote-native-terminal", Generation: 2, Sequence: 1, Data: "hello\n"})
	need(t, err)
	// Exercise at least one authorization renewal before revocation.
	time.Sleep(3500 * time.Millisecond)
	need(t, b.Services.Grants.Revoke(t.Context(), owner, policy.RevokeRequest{GrantID: ng.ID, Mutation: core.Mutation{IdempotencyKey: "revoke-network", ExpectedVersion: ng.Version}}))
	deadline = time.Now().Add(10 * time.Second)
	for {
		task, err := persistence.Load[shellTask](t.Context(), b.store, shellTasksBucket, persistence.Key(owner.OwnerID, e.OperationID))
		need(t, err)
		if task.State != "running" && task.State != "starting" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("remote native TTY survived revoke")
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Full Access is still a separate grant, never an automatic fallback.
	e.OperationID = "native-full-access"
	full := tools.ShellRequest{SandboxID: view.ID, ComputeID: compute, FullAccess: true, Script: `test -z "$MOLR_WORKSPACE" && echo FULL_OK`}
	if _, err := router.shellExecute(t.Context(), e, full); err == nil {
		t.Fatal("ungranted Full Access accepted")
	}
	grant(compute, "full-access", []policy.Action{policy.Execute}, policy.Scope{FullAccess: true}, nil)
	raw, err = router.shellExecute(t.Context(), e, full)
	need(t, err)
	if !strings.Contains(string(raw), "FULL_OK") {
		t.Fatalf("Full Access failed: %s", raw)
	}

	// Submit without renewals to simulate transport loss. Reading is side-effect free.
	connection, err := router.connection(t.Context(), owner, view.ID)
	need(t, err)
	client, err := router.client(t.Context(), owner, connection)
	need(t, err)
	epoch, err := router.epoch(t.Context(), owner.OwnerID)
	need(t, err)
	networkURL, err := url.Parse(target.NetworkURL)
	need(t, err)
	port, err := strconv.Atoi(networkURL.Port())
	need(t, err)
	e.OperationID = "native-lost-lease"
	args, _ := json.Marshal(tools.ShellRequest{Script: "sleep 60", ForwardPorts: []int{port}})
	q := sm.Request{Identity: identity(e), Epoch: epoch, Method: "shell_execute", Compute: true, ComputeConstraints: &policy.ComputeConstraints{MaxDurationSeconds: 60}, Networks: []string{"preset:network"}, NetworkAuthorizations: []platform.NetworkAuthorization{{Source: "preset:network", GrantID: "transport-test-grant"}}, Arguments: args}
	payload, _ := json.Marshal(q)
	req, err := http.NewRequestWithContext(t.Context(), "POST", client.Endpoint+"/v2/sandbox/submit", bytes.NewReader(payload))
	need(t, err)
	req.Header.Set("Authorization", "Bearer "+target.Token)
	response, err := client.HTTP.Do(req)
	need(t, err)
	var envelope struct {
		Data sm.Reply
		Code core.Code
	}
	need(t, json.NewDecoder(response.Body).Decode(&envelope))
	response.Body.Close()
	if response.StatusCode != 200 || envelope.Code != "" {
		t.Fatal("submit rejected", response.StatusCode, envelope.Code)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		stream, err := client.DialForward(t.Context(), q.Identity, port)
		if err == nil {
			_, err = fmt.Fprint(stream, "GET / HTTP/1.0\r\nHost: localhost\r\n\r\n")
			need(t, err)
			data, err := io.ReadAll(stream)
			stream.Close()
			need(t, err)
			if !strings.Contains(string(data), "NETWORK_OK") {
				t.Fatal("remote forward failed")
			}
			break
		}
		if time.Now().After(deadline) {
			reply, _ := client.Get(t.Context(), q.Identity)
			t.Fatalf("forward did not start: %v; operation: %+v", err, reply)
		}
		time.Sleep(100 * time.Millisecond)
	}
	deadline = time.Now().Add(sm.LeaseDuration + 5*time.Second)
	for {
		reply, err := client.Get(t.Context(), q.Identity)
		need(t, err)
		if reply.State != "running" {
			var result struct{ Exit platform.Exit }
			need(t, json.Unmarshal(reply.Result, &result))
			if result.Exit.Code == 0 {
				t.Fatal("lost lease did not cancel native process")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native process survived lost lease")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if stream, err := client.DialForward(t.Context(), q.Identity, port); err == nil {
		stream.Close()
		t.Fatal("forward survived expired lease")
	}

}
