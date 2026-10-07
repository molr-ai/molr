//go:build linux && (amd64 || arm64)

package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net"
	"strconv"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
)

// Readiness exercises namespace creation, DNS, policy, packet translation and
// cleanup with a private ephemeral loopback fixture. It never needs Internet.
func (r *Runner) probeNetwork(ctx context.Context) error {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return err
	}
	token := hex.EncodeToString(random[:])
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		buf := make([]byte, len(token))
		if _, err = io.ReadFull(conn, buf); err == nil && string(buf) == token {
			_, _ = conn.Write(buf)
		}
	}()
	defer func() { listener.Close(); <-done }()
	c := platformProbe()
	if r.options.NoWorkspace {
		c.Directory = "/tmp"
	}
	c.Timeout = 10 * time.Second
	c.NetworkTargets = []platform.NetworkTarget{{Protocols: []string{"tcp"}, Addresses: []string{"127.0.0.1"}, Ports: []string{port}}}
	c.Script = "/molr/helper sandbox-helper network-probe " + port + " " + token
	p, err := r.startAuthorized(ctx, probeExecution(), c, func(context.Context, core.Execution, platform.Command) error { return nil })
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, p.Stdout())
	_, _ = io.Copy(io.Discard, p.Stderr())
	exit, err := p.Wait(ctx)
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.Cancel(cleanup)
		return err
	}
	if exit.Code != 0 {
		return unavailable("sandbox_network_probe_failed")
	}
	return nil
}

func networkProbeClient(port, token string) int {
	if _, err := strconv.ParseUint(port, 10, 16); err != nil || len(token) != 32 {
		return 125
	}
	conn, err := net.DialTimeout("tcp", "host.molr.internal:"+port, 4*time.Second)
	if err != nil {
		return 125
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(4 * time.Second))
	if _, err = conn.Write([]byte(token)); err != nil {
		return 125
	}
	buf := make([]byte, len(token))
	if _, err = io.ReadFull(conn, buf); err != nil || string(buf) != token {
		return 125
	}
	return 0
}
