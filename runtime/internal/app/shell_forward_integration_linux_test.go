//go:build linux && (amd64 || arm64)

package app

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
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
	"molr.ai/runtime/internal/auth"
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
	mux.HandleFunc("/credentials", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "molr_dev_session_p8080=forged; Path=/")
		w.Header().Add("Set-Cookie", "application=kept; Path=/")
		w.Header().Set("Clear-Site-Data", `"cookies"`)
		json.NewEncoder(w).Encode(r.Header)
	})
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
	for _, mode := range []string{"local", "remote", "local_https"} {
		remote, name := mode == "remote", mode
		t.Run(name, func(t *testing.T) {
			b, owner, e := taskFixture(t)
			s := b.shellTasks
			s.forwardHTTP.TLSMode = config.LocalHTTP
			login, err := b.Services.Auth.Login(t.Context(), auth.LoginRequest{Username: "owner", Password: core.NewSecret("correct-horse-battery-staple-2026!"), Client: auth.Client{Address: "127.0.0.1", DeviceName: "forward-client"}})
			need(t, err)
			cookie := &http.Cookie{Name: "molr_dev_session_p8080", Value: login.Session.Token.Reveal()}
			forwardUser, err := b.Services.Auth.Authenticate(t.Context(), auth.Authentication{Session: login.Session.Token})
			need(t, err)
			s.forwardConfig = config.PortForward{Enabled: true, ListenHost: "127.0.0.1", PublicHost: "127.0.0.1"}
			var tlsConfig *tls.Config
			if mode == "local_https" {
				issuer := httptest.NewTLSServer(nil)
				defer issuer.Close()
				cert := issuer.TLS.Certificates[0]
				key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
				need(t, err)
				s.forwardConfig.CertFile = filepath.Join(t.TempDir(), "cert.pem")
				s.forwardConfig.KeyFile = filepath.Join(t.TempDir(), "key.pem")
				need(t, os.WriteFile(s.forwardConfig.CertFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0600))
				need(t, os.WriteFile(s.forwardConfig.KeyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600))
				roots := x509.NewCertPool()
				roots.AddCert(issuer.Certificate())
				tlsConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
			}
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
				client := &http.Client{Timeout: time.Second, Transport: &http.Transport{TLSClientConfig: tlsConfig}}
				defer client.CloseIdleConnections()
				deadline := time.Now().Add(15 * time.Second)
				for {
					request, _ := http.NewRequest("GET", task.Forwards[0].URL+"/nested/page", nil)
					request.AddCookie(cookie)
					res, err := client.Do(request)
					if err == nil && res.StatusCode == http.StatusOK {
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
				credentialReq, _ := http.NewRequest("GET", task.Forwards[0].URL+"/credentials", nil)
				credentialReq.AddCookie(cookie)
				credentialReq.AddCookie(&http.Cookie{Name: "application", Value: "kept"})
				credentialReq.Header.Set("Authorization", "Bearer private")
				credentialReq.Header.Set("X-CSRF-Token", "private")
				credentialRes, err := client.Do(credentialReq)
				need(t, err)
				var headers http.Header
				need(t, json.NewDecoder(credentialRes.Body).Decode(&headers))
				credentialRes.Body.Close()
				if headers.Get("Cookie") != "application=kept" || headers.Get("Authorization") != "" || headers.Get("X-CSRF-Token") != "" {
					t.Fatal("credential leak", headers)
				}
				if credentialRes.Header.Get("Clear-Site-Data") != "" {
					t.Fatal("upstream could clear Molr cookies")
				}
				for _, c := range credentialRes.Cookies() {
					if c.Name == cookie.Name {
						t.Fatal("upstream overwrote Molr session")
					}
				}
				unauth, err := client.Get(task.Forwards[0].URL + "/nested/page")
				need(t, err)
				unauth.Body.Close()
				if unauth.StatusCode != 401 {
					t.Fatalf("anonymous access: %d", unauth.StatusCode)
				}
				wsConfig, err := websocket.NewConfig(strings.Replace(task.Forwards[0].URL, "http", "ws", 1)+"/ws", task.Forwards[0].URL)
				need(t, err)
				wsConfig.TlsConfig = tlsConfig
				wsConfig.Header = http.Header{"Cookie": {cookie.String()}}
				ws, err := websocket.DialConfig(wsConfig)
				need(t, err)
				ws.SetDeadline(time.Now().Add(3 * time.Second))
				need(t, websocket.Message.Send(ws, "hmr-frame"))
				var echo string
				need(t, websocket.Message.Receive(ws, &echo))
				ws.Close()
				if echo != "hmr-frame" {
					t.Fatal(echo)
				}
				conn, err := asForwardConn(s.router.dialForward(t.Context(), operation, sandboxOf(nodeResource), 5174))
				need(t, err)
				conn.SetDeadline(time.Now().Add(4 * time.Second))
				payload := strings.Repeat("payload-", 12000)
				_, err = io.WriteString(conn, payload)
				need(t, err)
				need(t, conn.(interface{ CloseWrite() error }).CloseWrite())
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
			// Session revocation closes an already upgraded connection without stopping the task.
			wsConfig, err := websocket.NewConfig(strings.Replace(records[0].Forwards[0].URL, "http", "ws", 1)+"/ws", records[0].Forwards[0].URL)
			need(t, err)
			wsConfig.TlsConfig = tlsConfig
			wsConfig.Header = http.Header{"Cookie": {cookie.String()}}
			heldWS, err := websocket.DialConfig(wsConfig)
			need(t, err)
			need(t, b.Services.Auth.Logout(t.Context(), forwardUser))
			heldWS.SetReadDeadline(time.Now().Add(5 * time.Second))
			var value string
			if err := websocket.Message.Receive(heldWS, &value); err == nil {
				t.Fatal("websocket survived logout")
			} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
				t.Fatal("logout failed to disconnect websocket")
			}
			heldWS.Close()
			// Existing connections and listeners must both disappear on grant revocation.
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
