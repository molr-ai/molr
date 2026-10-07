//go:build linux && (amd64 || arm64)

package sandbox

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
)

func TestLinuxNetworkTTY(t *testing.T) {
	r := integrationRunner(t, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "tty-network-ok\n")
	}))
	defer server.Close()
	port := strconv.Itoa(server.Listener.Addr().(*net.TCPAddr).Port)
	c := networkCommand([]string{"tcp"}, port)
	c.TTY = true
	c.Script = "wget -qO- http://host.molr.internal:" + port
	if out := assertNetworkRun(t, r, c); !strings.Contains(out, "tty-network-ok") {
		t.Fatal(out)
	}
}

func networkCommand(protocols []string, ports ...string) platform.Command {
	c := testCommand()
	c.Timeout = 15 * time.Second
	c.MaxOutputBytes = 8192
	c.NetworkTargets = []platform.NetworkTarget{{Protocols: protocols, Addresses: []string{"127.0.0.1"}, Ports: ports}}
	return c
}
func prepareNetworkCA(t *testing.T, r *Runner, ca []byte) {
	t.Helper()
	e := testExecution()
	work := platform.AssistantWorkspace(r.options.PrivateRoot, r.options.WorkRoot, e.Principal.OwnerID, e.AssistantID)
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "ca.pem"), ca, 0600); err != nil {
		t.Fatal(err)
	}
}
func assertNetworkRun(t *testing.T, r *Runner, c platform.Command) string {
	t.Helper()
	out, exit := executeTest(t, r, c)
	if exit.Code != 0 {
		t.Fatalf("network execution failed: %+v %s", exit, out)
	}
	return out
}

func TestLinuxNetworkTLSBudgetAndBoundaries(t *testing.T) {
	r := integrationRunner(t, nil)
	server, ca := networkTestTLS(t, "127.0.0.1", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/large" {
			w.Header().Set("Content-Length", "1048576")
			io.WriteString(w, strings.Repeat("x", 1<<20))
			return
		}
		io.WriteString(w, "tls-ok")
	}))
	prepareNetworkCA(t, r, ca)
	port := strconv.Itoa(server.Listener.Addr().(*net.TCPAddr).Port)
	c := networkCommand([]string{"tcp"}, port)
	url := "https://host.molr.internal:" + port
	c.Script = "set -eu\n/molr/helper sandbox-network-client boundaries unused\n/molr/helper sandbox-test-syscalls\n/molr/helper sandbox-network-client untrusted " + url + "\n/molr/helper sandbox-network-client get " + url
	if out := assertNetworkRun(t, r, c); !strings.Contains(out, "https-dns-without-proxy-ok") {
		t.Fatal(out)
	}
	r.options.NetworkBytes = 32768
	c.Script = "/molr/helper sandbox-network-client budget " + url + "/large"
	if out := assertNetworkRun(t, r, c); !strings.Contains(out, "budget-ok") {
		t.Fatal(out)
	}
}

func TestLinuxNetworkUDPProtocolAndDefaultDeny(t *testing.T) {
	r := integrationRunner(t, nil)
	prepareNetworkCA(t, r, nil)
	echo, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		buf := make([]byte, 2048)
		for {
			n, a, e := echo.ReadFrom(buf)
			if e != nil {
				return
			}
			echo.WriteTo(buf[:n], a)
		}
	}()
	port := strconv.Itoa(echo.LocalAddr().(*net.UDPAddr).Port)
	c := networkCommand([]string{"udp"}, port)
	c.Script = "/molr/helper sandbox-network-client udp host.molr.internal:" + port
	assertNetworkRun(t, r, c)
	c.NetworkTargets[0].Protocols = []string{"tcp"}
	c.Script = "/molr/helper sandbox-network-client udp-denied host.molr.internal:" + port
	assertNetworkRun(t, r, c)
	c.NetworkTargets = nil
	c.Script = "/molr/helper sandbox-network-client denied http://127.0.0.1:" + port
	assertNetworkRun(t, r, c)
}

func TestLinuxNetworkRevocationClosesUpstream(t *testing.T) {
	var revoked atomic.Bool
	r := integrationRunner(t, func(context.Context, core.Execution, platform.Command) error {
		if revoked.Load() {
			return core.NewError(core.Forbidden, "revoked")
		}
		return nil
	})
	connected, closed := make(chan struct{}), make(chan struct{})
	server, ca := networkTestTLS(t, "127.0.0.1", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		io.WriteString(w, "connected\n")
		w.(http.Flusher).Flush()
		close(connected)
		<-req.Context().Done()
		close(closed)
	}))
	prepareNetworkCA(t, r, ca)
	port := strconv.Itoa(server.Listener.Addr().(*net.TCPAddr).Port)
	c := networkCommand([]string{"tcp"}, port)
	c.Script = "/molr/helper sandbox-network-client get https://host.molr.internal:" + port
	p, err := r.Start(t.Context(), testExecution(), c)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-connected:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not connect")
	}
	revoked.Store(true)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	exit, err := p.Wait(ctx)
	if err != nil || exit.Code != 125 {
		t.Fatalf("revocation: %+v %v", exit, err)
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("revoked upstream stayed open")
	}
	if _, err = r.Start(t.Context(), testExecution(), c); err == nil {
		t.Fatal("new command allowed after revocation")
	}
}

func TestLinuxNetworkGatewayFailureTerminatesExecution(t *testing.T) {
	r := integrationRunner(t, nil)
	c := networkCommand([]string{"tcp"}, "443")
	c.Script = "sleep 10"
	p, err := r.Start(t.Context(), testExecution(), c)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(r.options.CgroupRoot)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "molr-") {
			continue
		}
		path := filepath.Join(r.options.CgroupRoot, entry.Name())
		limit, _ := os.ReadFile(filepath.Join(path, "memory.max"))
		if strings.TrimSpace(string(limit)) == "268435456" {
			if err = os.WriteFile(filepath.Join(path, "cgroup.kill"), []byte("1"), 0600); err != nil {
				t.Fatal(err)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("network gateway lacks independent memory cgroup")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	exit, err := p.Wait(ctx)
	if err != nil || exit.Code != 125 || exit.Reason != "network_gateway_failed" {
		t.Fatalf("gateway failure: %+v %v", exit, err)
	}
	entries, _ = os.ReadDir(r.options.CgroupRoot)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "molr-") {
			t.Fatal("execution cgroup leaked", entry.Name())
		}
	}
}

func TestLinuxNetworkConnectionLimit(t *testing.T) {
	r := integrationRunner(t, nil)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	c := networkCommand([]string{"tcp"}, strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))
	c.Script = "/molr/helper sandbox-network-client connections host.molr.internal:" + strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	assertNetworkRun(t, r, c)
}

func TestLinuxNetworkHalfCloseTCPDNSAndIPv6(t *testing.T) {
	r := integrationRunner(t, nil)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		b, e := io.ReadAll(conn)
		if e == nil && string(b) == "request" {
			conn.Write([]byte("half-close-ok"))
		}
	}()
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	c := networkCommand([]string{"tcp"}, port)
	c.Script = "set -eu; /molr/helper sandbox-network-client dns-tcp host.molr.internal; /molr/helper sandbox-network-client half-close host.molr.internal:" + port
	assertNetworkRun(t, r, c)
	server, ca := networkTestTLS(t, "::1", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { io.WriteString(w, "tls-ok") }))
	prepareNetworkCA(t, r, ca)
	port = strconv.Itoa(server.Listener.Addr().(*net.TCPAddr).Port)
	c = networkCommand([]string{"tcp"}, port)
	c.NetworkTargets[0].Addresses = []string{"::1"}
	c.Script = "/molr/helper sandbox-network-client get https://host6.molr.internal:" + port
	assertNetworkRun(t, r, c)
}

func TestLinuxNetworkLargeAuthorizedPolicy(t *testing.T) {
	r := integrationRunner(t, nil)
	server, ca := networkTestTLS(t, "127.0.0.1", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { io.WriteString(w, "tls-ok") }))
	prepareNetworkCA(t, r, ca)
	port := strconv.Itoa(server.Listener.Addr().(*net.TCPAddr).Port)
	c := networkCommand([]string{"tcp"}, port)
	target := c.NetworkTargets[0]
	for i := 0; i < 31; i++ {
		target.Addresses = append(target.Addresses, strings.Repeat("a", 63)+".example.test")
	}
	c.NetworkTargets = nil
	for i := 0; i < 32; i++ {
		c.NetworkTargets = append(c.NetworkTargets, target)
	}
	c.Script = "/molr/helper sandbox-network-client get https://host.molr.internal:" + port
	assertNetworkRun(t, r, c)
}

func TestLinuxNetworkPublicHTTPS(t *testing.T) {
	if os.Getenv("MOLR_SANDBOX_PUBLIC_SMOKE") != "1" {
		t.Skip("optional external connectivity check")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(exe, 0755); err != nil {
		t.Fatal(err)
	}
	r := New(Options{QuotaStore: quotaTestStore(t), HostTools: os.Getenv("MOLR_SANDBOX_HOST_TOOLS") == "1", Helper: exe, PrivateRoot: t.TempDir(), CgroupRoot: os.Getenv("MOLR_SANDBOX_CGROUP"), MinFreeDiskBytes: 1, Authorize: func(context.Context, core.Execution, platform.Command) error { return nil }})
	defer r.Close()
	if err = r.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	c := testCommand()
	c.Timeout = 20 * time.Second
	c.NetworkTargets = []platform.NetworkTarget{{Protocols: []string{"tcp"}, Addresses: []string{"www.baidu.com"}, Ports: []string{"443"}, ExcludeAddresses: []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "fc00::/7", "::1/128"}}}
	c.Script = "set -eu; curl -q -fsSL --proto '=https' --proto-redir '=https' --max-time 15 -o /work/public.html https://www.baidu.com; test -s /work/public.html; echo public-https-ok"
	if out := assertNetworkRun(t, r, c); out != "public-https-ok\n" {
		t.Fatal(out)
	}
}

func TestLinuxNetworkQuotaSharedAcrossExecutions(t *testing.T) {
	r := integrationRunner(t, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, strings.Repeat("q", 16<<10)) }))
	defer server.Close()
	port := strconv.Itoa(server.Listener.Addr().(*net.TCPAddr).Port)
	c := networkCommand([]string{"tcp"}, port)
	c.Script = "wget -qO /tmp/download http://host.molr.internal:" + port + "; echo finished"
	c.NetworkAuthorizations = []platform.NetworkAuthorization{{Source: "test-shared", GrantID: "grant", Constraints: &policy.NetworkConstraints{Bytes: 24 << 10, Connections: 2, Period: "day", Timezone: "UTC"}}}
	assertNetworkRun(t, r, c)
	e := testExecution()
	before, err := r.NetworkUsage(t.Context(), e.Principal.OwnerID, e.AssistantID, "test-shared", *c.NetworkAuthorizations[0].Constraints)
	if err != nil || before.UsedBytes < 16<<10 {
		t.Fatal(before, err)
	}
	assertNetworkRun(t, r, c)
	after, err := r.NetworkUsage(t.Context(), e.Principal.OwnerID, e.AssistantID, "test-shared", *c.NetworkAuthorizations[0].Constraints)
	if err != nil || after.UsedBytes > 24<<10 || after.UsedBytes < before.UsedBytes || after.ReservedBytes != 0 {
		t.Fatal(after, err)
	}
	c.NetworkAuthorizations[0].Constraints.Bytes = 64 << 10
	assertNetworkRun(t, r, c)
	topped, err := r.NetworkUsage(t.Context(), e.Principal.OwnerID, e.AssistantID, "test-shared", *c.NetworkAuthorizations[0].Constraints)
	if err != nil || topped.UsedBytes <= after.UsedBytes {
		t.Fatal("topup lost prior usage", topped, err)
	}
}
