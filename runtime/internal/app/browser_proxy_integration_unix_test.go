//go:build (linux && (amd64 || arm64)) || darwin

package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/browser"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBrowserProxyRealSandbox(t *testing.T) {
	if os.Getenv("MOLR_SANDBOX_INTEGRATION") != "1" || os.Getenv("MOLR_TEST_AGENT_BROWSER") == "" {
		t.Skip("requires sandbox and pinned browser assets")
	}
	exe, err := os.Executable()
	need(t, err)
	need(t, os.Chmod(exe, 0755))
	var proxyHits, authHits, tunnels, socksHits atomic.Int64
	tlsTarget := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "TLS target") }))
	defer tlsTarget.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "CONNECT" {
			tunnels.Add(1)
			upstream, err := net.Dial("tcp", tlsTarget.Listener.Addr().String())
			if err != nil {
				http.Error(w, "failed", 502)
				return
			}
			client, rw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				upstream.Close()
				return
			}
			defer client.Close()
			defer upstream.Close()
			rw.WriteString("HTTP/1.1 200 Connection established\r\n\r\n")
			rw.Flush()
			go io.Copy(upstream, rw)
			io.Copy(client, upstream)
			return
		}
		proxyHits.Add(1)
		fmt.Fprint(w, "<!doctype html><title>Proxy fixture</title><h1>Reached HTTP proxy</h1>")
	}))
	defer proxy.Close()
	authenticated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("fixture-user:fixture-password"))
		if r.Header.Get("Proxy-Authorization") != expected {
			w.Header().Set("Proxy-Authenticate", `Basic realm="fixture"`)
			w.WriteHeader(407)
			return
		}
		authHits.Add(1)
		fmt.Fprint(w, "<!doctype html><h1>Authenticated proxy</h1>")
	}))
	defer authenticated.Close()
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<!doctype html><h1>Direct bypass</h1>") }))
	defer direct.Close()
	socks, err := net.Listen("tcp", "127.0.0.1:0")
	need(t, err)
	defer socks.Close()
	go func() {
		for {
			conn, err := socks.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(20 * time.Second))
				head := make([]byte, 2)
				if _, err := io.ReadFull(conn, head); err != nil {
					return
				}
				methods := make([]byte, int(head[1]))
				if _, err := io.ReadFull(conn, methods); err != nil {
					return
				}
				conn.Write([]byte{5, 0})
				header := make([]byte, 4)
				if _, err := io.ReadFull(conn, header); err != nil {
					return
				}
				n := 0
				switch header[3] {
				case 1:
					n = 4
				case 4:
					n = 16
				case 3:
					if _, err := io.ReadFull(conn, head[:1]); err != nil {
						return
					}
					n = int(head[0])
				default:
					return
				}
				address := make([]byte, n+2)
				if _, err := io.ReadFull(conn, address); err != nil {
					return
				}
				upstream, err := net.Dial("tcp", direct.Listener.Addr().String())
				if err != nil {
					return
				}
				defer upstream.Close()
				socksHits.Add(1)
				conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
				go io.Copy(upstream, conn)
				io.Copy(conn, upstream)
			}()
		}
	}()
	cfg := testConfig(t)
	cfg.Sandbox.HostTools = runtime.GOOS == "linux"
	cfg.Sandbox.CgroupRoot = os.Getenv("MOLR_SANDBOX_CGROUP")
	cfg.Browser.AgentDirectory = os.Getenv("MOLR_TEST_AGENT_BROWSER")
	cfg.Browser.ChromiumDirectory = os.Getenv("MOLR_TEST_CHROMIUM")
	ports := []string{}
	for _, addr := range []string{proxy.Listener.Addr().String(), authenticated.Listener.Addr().String(), direct.Listener.Addr().String(), socks.Addr().String()} {
		_, port, _ := net.SplitHostPort(addr)
		ports = append(ports, port)
	}
	networkTarget, host := "proxy-fixture", "host.molr.internal"
	computeLimits := &policy.ComputeConstraints{CPUPercent: 200, MemoryBytes: 2 << 30, Processes: 512, TempBytes: 512 << 20, MaxDurationSeconds: 300}
	if runtime.GOOS == "linux" {
		cfg.Sandbox.Networks = []config.SandboxNetwork{{Name: "proxy-fixture", Protocols: []string{"tcp"}, Addresses: []string{"127.0.0.1"}, Ports: ports}}
	} else {
		networkTarget, host = "preset:network", "127.0.0.1"
		computeLimits = &policy.ComputeConstraints{MaxDurationSeconds: 300}
	}
	b, err := Build(t.Context(), cfg)
	need(t, err)
	defer b.Close()
	owner, _ := ownerSession(t, b)
	e := core.Execution{Principal: core.Principal{OwnerID: owner.OwnerID, SubjectID: "proxy-assistant", Kind: core.ActorAssistant, ExecutionID: "proxy-run"}, AssistantID: "proxy-assistant", OperationID: "proxy-task"}
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "business.assistants", persistence.Key(owner.OwnerID, e.AssistantID), assistant.Assistant{ID: e.AssistantID, OwnerID: owner.OwnerID, State: assistant.Active, Version: 1}); err != nil {
			return err
		}
		return tx.Put(ctx, "agent.jobs", persistence.Key(owner.OwnerID, "proxy-job"), scheduler.Job{State: scheduler.Running, ExecutionID: e.Principal.ExecutionID, Event: event.AssistantEvent{ID: "proxy-job", OwnerID: owner.OwnerID, AssistantID: e.AssistantID}})
	}))
	need(t, b.sandbox.runner.Prepare(t.Context()))
	computeResource, err := b.Services.Resources.Connect(t.Context(), owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "proxy-compute-resource"}, Kind: resource.Compute, Provider: "local-sandbox", Name: "Proxy compute"})
	need(t, err)
	compute := computeResource.ID
	_, err = b.Services.Grants.Grant(t.Context(), owner, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "proxy-compute"}, AssistantID: e.AssistantID, ResourceID: compute, Actions: []policy.Action{policy.Execute}, Constraints: &policy.Constraints{Compute: computeLimits}})
	need(t, err)
	network, err := b.Services.Resources.Connect(t.Context(), owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "proxy-network"}, Kind: resource.Network, Provider: "sandbox-network", Name: "Proxy fixture", Binding: resource.Binding{SandboxTarget: networkTarget}})
	need(t, err)
	networkGrant, err := b.Services.Grants.Grant(t.Context(), owner, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "proxy-network-grant"}, AssistantID: e.AssistantID, ResourceID: network.ID, Actions: []policy.Action{policy.Execute}, Constraints: testResourceConstraints(resource.Network)})
	need(t, err)
	hostURL := func(raw string) string {
		u, _ := url.Parse(raw)
		u.Host = strings.Replace(u.Host, "127.0.0.1", host, 1)
		return u.String()
	}
	save := func(server string, bypass []string, auth bool) {
		old, err := b.shellTasks.loadBrowserProxy(t.Context())
		need(t, err)
		q := browserSettingsRequest{Mode: "proxy", Server: server, Bypass: bypass, PasswordAction: "clear", Version: old.Version}
		if auth {
			q.Username = "fixture-user"
			q.Password = "fixture-password"
			q.PasswordAction = "replace"
		}
		raw, _ := json.Marshal(q)
		_, err = b.shellTasks.browserSettings(t.Context(), owner, raw)
		need(t, err)
	}
	start := func(offline bool) *runningShell {
		e.OperationID = core.ID("proxy-task-" + strconv.FormatInt(time.Now().UnixNano(), 10))
		q := tools.BrowserRequest{Action: browser.Action{Action: "start"}, ComputeID: compute}
		if offline {
			q.NetworkMode = "none"
		}
		_, err := b.shellTasks.browserTool(t.Context(), e, "browser_session", q)
		if err != nil {
			t.Fatal("start", err)
		}
		r := b.shellTasks.activeBrowser(owner, e.AssistantID, "")
		if r == nil {
			t.Fatal("missing browser")
		}
		return r
	}
	stop := func(r *runningShell) {
		r.cancel()
		select {
		case <-r.done:
		case <-time.After(15 * time.Second):
			t.Fatal("browser did not stop")
		}
	}
	act := func(action, target string) (string, error) {
		raw, err := b.shellTasks.browserRequest(t.Context(), e.Principal, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: action, URL: target}})
		return string(raw), err
	}
	// A workspace config must not replace the administrator's proxy.
	work := platform.AssistantWorkspace(cfg.DataDir, cfg.Sandbox.WorkRoot, owner.OwnerID, e.AssistantID)
	need(t, os.MkdirAll(work, 0700))
	need(t, os.WriteFile(filepath.Join(work, "agent-browser.json"), []byte(`{"proxy":"http://untrusted.invalid:1"}`), 0600))
	save(hostURL(proxy.URL), []string{host}, false)
	r := start(false)
	_, err = act("navigate", "http://destination.invalid/")
	need(t, err)
	snapshot, err := act("snapshot", "")
	need(t, err)
	if !strings.Contains(snapshot, "Reached HTTP proxy") || proxyHits.Load() == 0 {
		t.Fatal(snapshot)
	}
	_, err = act("navigate", hostURL(direct.URL))
	need(t, err)
	snapshot, err = act("snapshot", "")
	need(t, err)
	if !strings.Contains(snapshot, "Direct bypass") {
		t.Fatal(snapshot)
	}
	_, err = act("navigate", "https://destination.invalid/")
	if err == nil || tunnels.Load() == 0 || !strings.Contains(err.Error(), "ERR_CERT_AUTHORITY_INVALID") {
		t.Fatalf("HTTPS tunnel must reach fixture while retaining certificate verification: %v (tunnels=%d)", err, tunnels.Load())
	}
	stop(r)
	save(hostURL(authenticated.URL), nil, true)
	r = start(false)
	_, err = act("navigate", "http://destination.invalid/")
	need(t, err)
	if authHits.Load() == 0 {
		t.Fatal("authentication missing")
	}
	stop(r)
	save("socks5://"+host+":"+ports[3], nil, false)
	r = start(false)
	_, err = act("navigate", "http://destination.invalid/")
	need(t, err)
	if socksHits.Load() == 0 {
		t.Fatal("SOCKS unused")
	}
	stop(r)
	// Undeclared endpoint and explicit offline mode cannot acquire network access.
	save("http://"+host+":1", nil, false)
	r = start(false)
	_, err = act("navigate", "http://destination.invalid/")
	if err == nil {
		t.Fatal("unauthorized proxy allowed")
	}
	if runtime.GOOS == "linux" && !strings.Contains(err.Error(), "network_destination_denied") {
		t.Fatal("sandbox denial not surfaced", err)
	}
	stop(r)
	save(hostURL(proxy.URL), nil, false)
	r = start(true)
	before := proxyHits.Load()
	_, err = act("navigate", "http://destination.invalid/")
	if err == nil || proxyHits.Load() != before {
		t.Fatal("offline browser contacted proxy", err)
	}
	stop(r)
	r = start(false)
	_, err = act("navigate", "http://destination.invalid/")
	need(t, err)
	need(t, b.Services.Grants.Revoke(t.Context(), owner, policy.RevokeRequest{GrantID: networkGrant.ID, Mutation: core.Mutation{IdempotencyKey: "proxy-revoke", ExpectedVersion: networkGrant.Version}}))
	before = proxyHits.Load()
	_, err = act("navigate", "http://revoked.invalid/")
	if err == nil || proxyHits.Load() != before {
		t.Fatal("revoked network still reached proxy", err)
	}
	if runtime.GOOS == "linux" && !strings.Contains(err.Error(), "network_authorization_changed") {
		t.Fatal("revocation diagnostic missing", err)
	}
	stop(r)
}
