package browser

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDaemonPeerIdentityBeforeSendingCommand(t *testing.T) {
	root, err := os.MkdirTemp("/private/tmp", "molr-peer-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	socket := filepath.Join(root, "daemon.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	for _, correct := range []bool{true, false} {
		received := make(chan bool, 1)
		go func() {
			conn, err := listener.Accept()
			if err != nil {
				received <- false
				return
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(2 * time.Second))
			var cmd map[string]any
			err = json.NewDecoder(conn).Decode(&cmd)
			received <- err == nil
			io.WriteString(conn, "{\"success\":true,\"data\":{}}\n")
		}()
		pid := os.Getpid()
		if !correct {
			pid++
		}
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		result, err := Command(ctx, socket, pid, map[string]any{"action": "launch", "proxy": "fixture-secret"})
		cancel()
		if (err == nil) != correct || result.OK != correct {
			t.Fatalf("correct=%v %+v %v", correct, result, err)
		}
		if <-received != correct {
			t.Fatal("command sent to an untrusted socket peer")
		}
	}
}
