//go:build linux && (amd64 || arm64)

package sandbox

import (
	"net"
	"os"
	"testing"
)

func TestBrowserNativePortOwnershipAndSandboxProbe(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	if !browserOwnsPort(t.Context(), os.Getpid(), port) {
		t.Fatal("owned listener not recognized")
	}
	if browserOwnsPort(t.Context(), os.Getpid()+1000000, port) {
		t.Fatal("foreign process accepted")
	}
	if browserRendererSandboxed(t.Context(), os.Getpid()) {
		t.Fatal("ordinary Runtime process accepted as Chromium renderer")
	}
}
