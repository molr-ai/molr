//go:build linux && (amd64 || arm64)

package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	sm "molr.ai/runtime/internal/sandboxmanager"
)

// Runs as the untrusted command inside the real sandbox, never on the host.
func shellForwardTestServer(marker string) {
	listener, err := net.Listen("tcp4", "127.0.0.1:5174")
	if err != nil {
		panic(err)
	}
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				b, _ := io.ReadAll(io.LimitReader(c, 2<<20))
				c.Write(append([]byte(marker+":"), b...))
			}()
		}
	}()
	mux := http.NewServeMux()
	mux.Handle("/ws", websocket.Handler(func(c *websocket.Conn) { defer c.Close(); io.Copy(c, c) }))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, marker+":"+r.URL.Path)
	})
	if err := http.ListenAndServe("127.0.0.1:5173", mux); err != nil {
		panic(err)
	}
}

func forwardCompute(t *testing.T, b *Bundle, owner core.Principal, remote bool) core.ID {
	t.Helper()
	if !remote {
		need(t, b.sandbox.runner.Prepare(t.Context()))
		compute, err := b.Services.Resources.Connect(t.Context(), owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "forward-compute"}, Kind: resource.Compute, Provider: "local-sandbox", Name: "Forward compute"})
		need(t, err)
		return compute.ID
	}
	cfg := testConfig(t)
	backend, err := newSandboxBackend(t.Context(), cfg, quotaTestStore(t))
	need(t, err)
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "remote.db"))
	need(t, err)
	manager, err := sm.New(t.Context(), store, tracedTestBackend{backend, t})
	need(t, err)
	token := strings.Repeat("forward-test-", 8)
	handler, err := sm.Handler(manager, core.NewSecret(token))
	need(t, err)
	server := httptest.NewTLSServer(handler)
	t.Cleanup(func() { manager.Close(); server.Close(); store.Close() })
	fingerprint := sha256.Sum256(server.Certificate().Raw)
	router := b.shellTasks.router
	raw, _ := json.Marshal(map[string]any{"Name": "Forward remote", "Endpoint": server.URL, "Fingerprint": hex.EncodeToString(fingerprint[:]), "Token": token, "Mutation": core.Mutation{IdempotencyKey: "forward-remote"}})
	view, err := router.admin(t.Context(), owner, "connect", raw)
	need(t, err)
	id := view.(sandboxView).ID
	raw, _ = json.Marshal(map[string]any{"SandboxID": id, "Kind": "compute", "SourceID": "compute", "Name": "Remote compute", "Mutation": core.Mutation{IdempotencyKey: "forward-compute"}})
	_, err = router.admin(t.Context(), owner, "resource", raw)
	need(t, err)
	rows, err := persistence.Scan[resource.Resource](t.Context(), b.store, "business.resources")
	need(t, err)
	for _, r := range rows {
		if r.SandboxID == id && r.Kind == resource.Compute {
			return r.ID
		}
	}
	t.Fatal("remote compute missing")
	return ""
}

func TestShellForwardRealLocalAndRemote(t *testing.T) {
	if os.Getenv("MOLR_SANDBOX_INTEGRATION") != "1" {
		t.Skip("real sandbox required")
	}
	exe, err := os.Executable()
	need(t, err)
	need(t, os.Chmod(exe, 0755))
	for _, remote := range []bool{false, true} {
		name := "local"
		if remote {
			name = "remote"
		}
		t.Run(name, func(t *testing.T) {
			b, owner, e := taskFixture(t)
			s := b.shellTasks
			s.forwardConfig = config.PortForward{Enabled: true, ListenHost: "127.0.0.1", PublicHost: "127.0.0.1"}
			compute := forwardCompute(t, b, owner, remote)
			grant, err := b.Services.Grants.Grant(t.Context(), owner, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "forward-grant"}, AssistantID: e.AssistantID, ResourceID: compute, Actions: []policy.Action{policy.Execute}, Constraints: testResourceConstraints(resource.Compute)})
			need(t, err)
			var records []shellTask
			for i, mode := range []string{"background", "tty"} {
				operation := e
				e.OperationID = core.ID("forward-" + mode)
				operation.OperationID = e.OperationID
				nodeResource, err := resource.NewRepository(b.store).Get(t.Context(), owner.OwnerID, compute)
				need(t, err)
				req := tools.ShellRequest{SandboxID: sandboxOf(nodeResource), ComputeID: compute, Script: "exec /molr/helper shell-forward-test-server " + mode, ForwardPorts: []int{5173, 5174}, Background: i == 0, TTY: i == 1}
				raw, err := s.execute(t.Context(), operation, req)
				need(t, err)
				var receipt struct{ Task shellTask }
				need(t, json.Unmarshal(raw, &receipt))
				if len(receipt.Task.Forwards) != 2 {
					t.Fatalf("missing forward receipt: %s", raw)
				}
				task := receipt.Task
				client := &http.Client{Timeout: time.Second}
				deadline := time.Now().Add(15 * time.Second)
				for {
					res, err := client.Get(task.Forwards[0].URL + "/nested/page")
					if err == nil {
						body, _ := io.ReadAll(res.Body)
						res.Body.Close()
						if string(body) != mode+":/nested/page" {
							t.Fatalf("wrong namespace: %s", body)
						}
						break
					}
					if time.Now().After(deadline) {
						row, _ := persistence.Load[shellTask](t.Context(), b.store, shellTasksBucket, persistence.Key(owner.OwnerID, task.ID))
						t.Fatalf("HTTP unavailable: %v task=%+v", err, row)
					}
					time.Sleep(100 * time.Millisecond)
				}
				ws, err := websocket.Dial(strings.Replace(task.Forwards[0].URL, "http:", "ws:", 1)+"/ws", "", task.Forwards[0].URL)
				need(t, err)
				ws.SetDeadline(time.Now().Add(3 * time.Second))
				need(t, websocket.Message.Send(ws, "hmr-frame"))
				var echo string
				need(t, websocket.Message.Receive(ws, &echo))
				ws.Close()
				if echo != "hmr-frame" {
					t.Fatal(echo)
				}
				conn, err := net.DialTimeout("tcp", task.Forwards[1].Address, time.Second)
				need(t, err)
				conn.SetDeadline(time.Now().Add(4 * time.Second))
				payload := strings.Repeat("payload-", 12000)
				_, err = io.WriteString(conn, payload)
				need(t, err)
				need(t, conn.(*net.TCPConn).CloseWrite())
				result, err := io.ReadAll(conn)
				conn.Close()
				need(t, err)
				if string(result) != mode+":"+payload {
					t.Fatalf("half-close lost bytes: %d", len(result))
				}
				s.mu.Lock()
				id := s.active[shellKey(operation)].task.SandboxID
				s.mu.Unlock()
				if c, err := s.router.dialForward(t.Context(), operation, id, 5175); err == nil {
					c.Close()
					t.Fatal("undeclared port accessible")
				}
				forged := operation
				forged.Principal.ExecutionID = "another-execution"
				if c, err := s.router.dialForward(t.Context(), forged, id, 5173); err == nil {
					c.Close()
					t.Fatal("wrong execution accessible")
				}
				records = append(records, task)
			}
			if records[0].Forwards[0].Address == records[1].Forwards[0].Address {
				t.Fatal("tasks share external port")
			}
			// Existing connections and listeners must both disappear on revocation.
			held, err := net.DialTimeout("tcp", records[1].Forwards[1].Address, time.Second)
			need(t, err)
			defer held.Close()
			_, err = held.Write([]byte("waiting"))
			need(t, err)
			need(t, b.Services.Grants.Revoke(t.Context(), owner, policy.RevokeRequest{Mutation: core.Mutation{IdempotencyKey: "forward-revoke", ExpectedVersion: grant.Version}, GrantID: grant.ID}))
			held.SetReadDeadline(time.Now().Add(5 * time.Second))
			var buf [1]byte
			if _, err := held.Read(buf[:]); err == nil {
				t.Fatal("connection survived revocation")
			} else if n, ok := err.(net.Error); ok && n.Timeout() {
				t.Fatal("revocation did not close connection")
			}
			deadline := time.Now().Add(6 * time.Second)
			for _, record := range records {
				for {
					row, err := persistence.Load[shellTask](t.Context(), b.store, shellTasksBucket, persistence.Key(owner.OwnerID, record.ID))
					need(t, err)
					if row.State != "running" {
						for _, f := range row.Forwards {
							if f.State != "closed" {
								t.Fatal(f)
							}
						}
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("task survived revocation")
					}
					time.Sleep(30 * time.Millisecond)
				}
				if c, err := net.DialTimeout("tcp", record.Forwards[0].Address, time.Second); err == nil {
					c.Close()
					t.Fatal("listener survived task")
				}
			}
		})
	}
}

type tracedTestBackend struct {
	*linuxSandboxBackend
	t *testing.T
}

func (b tracedTestBackend) Call(ctx context.Context, r sm.Request) (json.RawMessage, error) {
	out, err := b.linuxSandboxBackend.Call(ctx, r)
	if err != nil {
		b.t.Logf("remote %s: %v", r.Method, err)
	}
	return out, err
}
