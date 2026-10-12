//go:build linux && (amd64 || arm64)

package sandbox

import (
	"fmt"
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

	"molr.ai/runtime/internal/platform"
)

func TestLinuxPrivateSockets(t *testing.T) {
	r := hostToolsRunner(t)
	source := t.TempDir()
	host, err := net.Listen("unix", filepath.Join(source, "host.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	abstract, err := net.Listen("unix", fmt.Sprintf("@molr-host-%d", os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	defer abstract.Close()
	c := testCommand()
	c.Timeout = 20 * time.Second
	c.Limits.MemoryBytes = 512 << 20
	c.Limits.Processes = 64
	c.Mounts = []platform.Mount{{Source: source, Destination: "/resources/ipc"}}
	c.Script = fmt.Sprintf(`set -eu
python3 - <<'PY'
import socket,os,errno,threading,time
for kind in (socket.SOCK_STREAM,socket.SOCK_SEQPACKET):
 a,b=socket.socketpair(type=kind);a.send(b'pair');assert b.recv(4)==b'pair';a.close();b.close()
 for name in ('/tmp/socket-'+str(kind),'\0molr-private-'+str(kind)):
  server=socket.socket(socket.AF_UNIX,kind);server.bind(name);server.listen()
  client=socket.socket(socket.AF_UNIX,kind);client.connect(name)
  peer,_=server.accept();client.send(b'private');assert peer.recv(7)==b'private'
  peer.close();client.close();server.close()
for create in (lambda:socket.socket(socket.AF_UNIX,socket.SOCK_DGRAM),lambda:socket.socketpair(type=socket.SOCK_DGRAM)):
 try: create()
 except OSError as e: assert e.errno==errno.EPERM,e
 else: raise AssertionError('datagram allowed')
os.symlink('/resources/ipc/host.sock','/tmp/host-link')
os.symlink('/resources/ipc','/tmp/host-dir')
for name in ('/resources/ipc/host.sock','/tmp/host-link','/tmp/host-dir/host.sock','/tmp/../resources/ipc/host.sock','/proc/self/root/resources/ipc/host.sock','\0molr-host-%d'):
 s=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM)
 try: s.connect(name)
 except OSError: pass
 else: raise AssertionError('host socket reached: '+name)
 finally: s.close()
for name in ('/resources/ipc/new.sock','/tmp/host-dir/new.sock','/tmp/../resources/ipc/new.sock'):
 s=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM)
 try: s.bind(name)
 except OSError: pass
 else: raise AssertionError('host socket bound: '+name)
 finally:s.close()
# Double-forked daemons must remain readable by the trusted IPC broker under Yama.
server=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM);server.bind('/tmp/daemon');server.listen();server.settimeout(3)
child=os.fork()
if child==0:
 if os.fork()!=0:os._exit(0)
 parent=os.getppid()
 time.sleep(.05)
 client=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM);client.connect('/tmp/daemon');client.send(b'daemon')
 os._exit(0)
os.waitpid(child,0)
peer,_=server.accept();assert peer.recv(6)==b'daemon';peer.close();server.close()
# Race a pathname between an execution-local endpoint and a host symlink.
s=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM);s.bind('/tmp/local');s.listen(256)
stop=threading.Event()
def swap():
 while not stop.is_set():
  try:
   os.rename('/tmp/local','/tmp/race');os.symlink('/resources/ipc/host.sock','/tmp/local')
   os.unlink('/tmp/local');os.rename('/tmp/race','/tmp/local')
  except FileNotFoundError: pass
thread=threading.Thread(target=swap);thread.start()
try:
 for i in range(100):
  probe=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM);probe.settimeout(.05)
  try: probe.connect('/tmp/local')
  except OSError: pass
  finally:probe.close()
finally:stop.set();thread.join()
print('private-sockets-ok')
PY`, os.Getpid())
	if out := assertNetworkRun(t, r, c); !strings.Contains(out, "private-sockets-ok") {
		t.Fatal(out)
	}
	if _, err := os.Stat(filepath.Join(source, "new.sock")); !os.IsNotExist(err) {
		t.Fatalf("host bind: %v", err)
	}
	host.(*net.UnixListener).SetDeadline(time.Now().Add(20 * time.Millisecond))
	if conn, err := host.Accept(); err == nil {
		conn.Close()
		t.Fatal("host socket received sandbox connection")
	}
}

// Supply reviewed program assets explicitly; never download code during tests.
func TestLinuxAgentBrowser(t *testing.T) {
	agent, chrome := os.Getenv("MOLR_TEST_AGENT_BROWSER"), os.Getenv("MOLR_TEST_CHROMIUM")
	if agent == "" || chrome == "" {
		t.Skip("requires agent-browser binary directory and Chromium directory")
	}
	r := hostToolsRunner(t)
	c := testCommand()
	c.Timeout = 90 * time.Second
	c.Limits = platform.Limits{MemoryBytes: 2 << 30, Processes: 512, CPUPercent: 200, TempBytes: 512 << 20, MaxFileBytes: 256 << 20, NetworkBytes: 64 << 20, NetworkConnections: 16}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "<title>Authorized network</title>") }))
	defer server.Close()
	var deniedHits atomic.Int32
	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { deniedHits.Add(1) }))
	defer denied.Close()
	port := strconv.Itoa(server.Listener.Addr().(*net.TCPAddr).Port)
	deniedPort := strconv.Itoa(denied.Listener.Addr().(*net.TCPAddr).Port)
	c.NetworkTargets = []platform.NetworkTarget{{Protocols: []string{"tcp"}, Addresses: []string{"127.0.0.1"}, Ports: []string{port}}}
	c.Mounts = []platform.Mount{{Source: agent, Destination: "/resources/agent", ReadOnly: true}, {Source: chrome, Destination: "/resources/chromium", ReadOnly: true}}
	c.Script = `set -eu
export AGENT_BROWSER_SOCKET_DIR=/tmp/agent-browser
export AGENT_BROWSER_EXECUTABLE_PATH=/resources/chromium/chrome
export AGENT_BROWSER_ARGS=--no-sandbox,--disable-dev-shm-usage
export TOKIO_WORKER_THREADS=2 RAYON_NUM_THREADS=2
mkdir -p "$AGENT_BROWSER_SOCKET_DIR"
cat > /work/browser.html <<'HTML'
<!doctype html><title>Sandbox browser acceptance</title><label>Name <input id="name"></label><button onclick="document.querySelector('#result').textContent='Hello '+document.querySelector('#name').value">Submit</button><p id="result">Waiting</p>
HTML
ab() { /resources/agent/agent-browser-linux-x64 "$@"; }
trap 'ab close >/dev/null 2>&1 || true' EXIT
ab --version
ab open file:///work/browser.html
ab snapshot -i
ab fill '#name' Molr
ab click 'button'
ab get text '#result' | grep 'Hello Molr'
ab screenshot /work/browser.png
test -s /work/browser.png
ab open http://host.molr.internal:ALLOWED_PORT
ab get title | grep 'Authorized network'
ab eval "fetch('http://host.molr.internal:DENIED_PORT').then(()=>'unexpected',()=> 'network-denied')" | grep network-denied
ab close
echo agent-browser-ok
`
	c.Script = strings.ReplaceAll(strings.ReplaceAll(c.Script, "ALLOWED_PORT", port), "DENIED_PORT", deniedPort)
	if out := assertNetworkRun(t, r, c); !strings.Contains(out, "agent-browser-ok") {
		t.Fatal(out)
	} else {
		t.Log(out)
	}
	if deniedHits.Load() != 0 {
		t.Fatal("browser reached unauthorized endpoint")
	}
}
