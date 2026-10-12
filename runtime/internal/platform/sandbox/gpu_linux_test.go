//go:build linux && (amd64 || arm64)

package sandbox

import (
	"context"
	"golang.org/x/sys/unix"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
)

const fixtureGPU = "GPU-00000000-0000-0000-0000-000000000001"

func TestGPUInventoryValidation(t *testing.T) {
	good := fixtureGPU + ", Example GPU, 8192, 3\n"
	devices, nodes, err := parseNVIDIAInventory([]byte(good))
	if err != nil || len(devices) != 1 || devices[0].MemoryBytes != 8<<30 || nodes[fixtureGPU].Path != "/dev/nvidia3" {
		t.Fatalf("%+v %+v %v", devices, nodes, err)
	}
	for _, bad := range []string{good + good, "all, GPU, 8, 0", fixtureGPU + ", GPU, -1, 0", fixtureGPU + ", GPU, 8, ../../dev"} {
		if _, _, err := parseNVIDIAInventory([]byte(bad)); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
func TestGPUAccessFailsClosed(t *testing.T) {
	r := New(Options{})
	if r.checkGPUAccess(platform.Command{}) != nil {
		t.Fatal("CPU-only execution blocked")
	}
	c := platform.Command{GPUDeviceIDs: []string{fixtureGPU}}
	if r.checkGPUAccess(c) == nil {
		t.Fatal("GPU enabled by request")
	}
	r.options.GPU = platform.GPUConfig{Enabled: true, Devices: []string{fixtureGPU}}
	if r.checkGPUAccess(c) == nil {
		t.Fatal("unprobed GPU permitted")
	}
	r.gpuDevices = []platform.GPUDevice{{ID: fixtureGPU, Available: true}}
	if err := r.checkGPUAccess(c); err != nil {
		t.Fatal(err)
	}
	c.FullAccess = true
	if r.checkGPUAccess(c) == nil {
		t.Fatal("GPU scope falsely accepted for Full Access")
	}
}

func TestLinuxGPUIsolation(t *testing.T) {
	if os.Getenv("MOLR_GPU_INTEGRATION") != "1" {
		t.Skip("requires NVIDIA GPUs and delegated cgroup")
	}
	devices, _, err := nvidiaInventory(t.Context())
	if err != nil || len(devices) < 2 {
		t.Fatalf("two GPUs required: %v", err)
	}
	ids := []string{devices[0].ID, devices[1].ID}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(exe, 0755); err != nil {
		t.Fatal(err)
	}
	var allowed atomic.Bool
	allowed.Store(true)
	r := New(Options{Bwrap: os.Getenv("MOLR_TEST_BWRAP"), QuotaStore: quotaTestStore(t), GPU: platform.GPUConfig{Enabled: true, Devices: ids}, HostTools: true, PrivateRoot: t.TempDir(), Helper: exe, MinFreeDiskBytes: 1, Authorize: func(context.Context, core.Execution, platform.Command) error {
		if !allowed.Load() {
			return unavailable("test_revoked")
		}
		return nil
	}})

	defer r.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	if err := r.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	for _, d := range r.GPUs() {
		if !d.Available {
			c := platformProbe()
			c.GPUDeviceIDs = []string{d.ID}
			c.Script = gpuProbeScript(c.GPUDeviceIDs)
			c.Limits.MemoryBytes = 1 << 30
			c.Limits.Processes = 128
			p, err := r.startAuthorized(ctx, probeExecution(), c, func(context.Context, core.Execution, platform.Command) error { return nil })
			if err != nil {
				t.Fatalf("probe %s: %s: %v", d.Name, d.Reason, err)
			}
			out, _ := io.ReadAll(p.Stdout())
			stderr, _ := io.ReadAll(p.Stderr())
			result, err := p.Wait(ctx)
			t.Fatalf("probe %s: %s: %+v %v stdout=%s stderr=%s", d.Name, d.Reason, result, err, out, stderr)
		}
	}
	c := testCommand()
	c.Limits.MemoryBytes = 1 << 30
	c.Limits.Processes = 128
	c.Timeout = 15 * time.Second
	c.Script = "test ! -e /dev/nvidiactl && test ! -e /dev/nvidia0 && test ! -e /dev/nvidia1 && echo cpu-only"
	out, result := executeTest(t, r, c)
	if result.Code != 0 || !strings.Contains(out, "cpu-only") {
		t.Fatalf("CPU leaked devices: %+v %s", result, out)
	}
	for _, selected := range [][]string{{ids[0]}, {ids[1]}, ids} {
		c.GPUDeviceIDs = selected
		c.Script = "export CUDA_VISIBLE_DEVICES=0,1 NVIDIA_VISIBLE_DEVICES=all\n" + gpuProbeScript(selected)
		out, result := executeTest(t, r, c)
		if result.Code != 0 || !strings.Contains(out, "gpu-probe-ok") {
			t.Fatalf("GPU boundary: %+v %s", result, out)
		}
	}
	// The CUDA-required socket type cannot reach pathname sockets on the host.
	root := t.TempDir()
	listener, err := net.Listen("unixpacket", filepath.Join(root, "host.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	c.GPUDeviceIDs = ids[:1]
	c.Mounts = []platform.Mount{{Source: root, Destination: "/resources/ipc"}}
	c.Script = `/usr/bin/python3 - <<'PYGPU'
import socket, errno
s=socket.socket(socket.AF_UNIX,socket.SOCK_SEQPACKET)
try:
    s.connect('/resources/ipc/host.sock')
except OSError as e:
    assert e.errno==errno.EACCES,e
else:
    raise AssertionError('host socket reached')
s.close()
s=socket.socket(socket.AF_UNIX,socket.SOCK_SEQPACKET)
try:
    s.bind('/resources/ipc/child.sock')
except OSError as e:
    assert e.errno==errno.EACCES,e
else:
    raise AssertionError('host-visible socket created')
s.close()
server=socket.socket(socket.AF_UNIX,socket.SOCK_SEQPACKET)
server.bind('\0molr-gpu-test');server.listen(1)
client=socket.socket(socket.AF_UNIX,socket.SOCK_SEQPACKET)
client.connect('\0molr-gpu-test')
peer,_=server.accept();client.send(b'ok');assert peer.recv(2)==b'ok'
print('ipc-isolated')
PYGPU`
	out, result = executeTest(t, r, c)
	if result.Code != 0 || !strings.Contains(out, "ipc-isolated") {
		t.Fatalf("IPC boundary: %+v %s", result, out)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("gpu-network-ok")) }))
	defer server.Close()
	address, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(address.Port())
	c.NetworkTargets = []platform.NetworkTarget{{Protocols: []string{"tcp"}, Addresses: []string{"127.0.0.1"}, Ports: []string{strconv.Itoa(port)}}}
	c.Script = "curl --silent --show-error --fail --max-time 5 http://host.molr.internal:" + strconv.Itoa(port)
	out, result = executeTest(t, r, c)
	if result.Code != 0 || !strings.Contains(out, "gpu-network-ok") {
		t.Fatalf("GPU TCP: %+v %s", result, out)
	}
	c.GPUDeviceIDs = []string{fixtureGPU}
	if _, err := r.Start(ctx, testExecution(), c); err == nil {
		t.Fatal("unconfigured device allowed")
	}
	c.GPUDeviceIDs = ids[:1]
	c.NetworkTargets = nil
	c.Mounts = nil
	c.Script = "echo gpu-running; sleep 30 & wait"
	c.TTY = true
	p, err := r.Start(ctx, testExecution(), c)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 256)
	n, err := p.Stdout().Read(buf)
	if err != nil || !strings.Contains(string(buf[:n]), "gpu-running") {
		t.Fatalf("TTY not ready: %s %v", buf[:n], err)
	}
	allowed.Store(false)
	result, err = p.Wait(ctx)
	if err != nil || result.Reason != "authorization_changed" {
		t.Fatalf("GPU TTY revoke: %+v %v", result, err)
	}

}

func TestGPUConnectAddressPolicy(t *testing.T) {
	for _, tc := range []struct {
		domain  int
		address []byte
		allowed bool
	}{
		{unix.AF_UNIX, []byte{1, 0, 0, 'a'}, true},
		{unix.AF_UNIX, []byte{1, 0, '/', 'a'}, false},
		{unix.AF_UNIX, []byte{2, 0, 0, 'a'}, false},
		{unix.AF_UNIX, []byte{1, 0}, false},
		{unix.AF_INET, append([]byte{2, 0}, make([]byte, 14)...), true},
		{unix.AF_INET, []byte{1, 0, 0, 'a'}, false},
	} {
		if actual := connectAddressAllowed(tc.domain, tc.address); actual != tc.allowed {
			t.Fatalf("domain %d address %v: %v", tc.domain, tc.address, actual)
		}
	}
}
