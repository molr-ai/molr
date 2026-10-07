package sandboxmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/portforward"
)

type forwardRequest struct {
	Identity Identity
	Port     int
}
type forwardBackend interface {
	DialForward(context.Context, Identity, int) (net.Conn, error)
}

func (m *Manager) dialForward(ctx context.Context, q forwardRequest) (net.Conn, error) {
	if err := q.Identity.Execution().Validate(); err != nil {
		return nil, err
	}
	if q.Port < 1 || q.Port > 65535 {
		return nil, core.Invalid("port", "invalid TCP port")
	}
	m.mu.Lock()
	a := m.active[operationKey(q.Identity)]
	allowed := !m.closed && a != nil && a.identity == q.Identity && time.Now().Before(a.lease)
	m.mu.Unlock()
	if !allowed {
		return nil, core.NewError(core.Forbidden, "forward task unavailable")
	}
	b, ok := m.Backend.(forwardBackend)
	if !ok {
		return nil, core.NewError(core.FailedPrecondition, "port forwarding unsupported")
	}
	return b.DialForward(ctx, q.Identity, q.Port)
}

type bufferedConn struct {
	net.Conn
	reader io.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

func (m *Manager) forwardHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !strings.EqualFold(r.Header.Get("Upgrade"), "molr-tcp-v1") || !strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") {
		http.Error(w, "upgrade required", 400)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var q forwardRequest
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&q) != nil {
		http.Error(w, "invalid forward", 400)
		return
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		http.Error(w, "invalid forward", 400)
		return
	}
	conn, err := m.dialForward(r.Context(), q)
	if err != nil {
		http.Error(w, "forward unavailable", http.StatusServiceUnavailable)
		return
	}
	defer conn.Close()
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "HTTP/1 upgrade required", 400)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	client.SetDeadline(time.Time{})
	if _, err = buffered.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: molr-tcp-v1\r\n\r\n"); err != nil {
		return
	}
	if buffered.Flush() != nil {
		return
	}
	upstream, ok := conn.(portforward.Stream)
	if !ok {
		return
	}
	portforward.Bridge(&portforward.Framed{ReadWriteCloser: &bufferedConn{Conn: client, reader: buffered.Reader}}, upstream)
}

func (c *Client) DialForward(ctx context.Context, id Identity, port int) (portforward.Stream, error) {
	raw, _ := json.Marshal(forwardRequest{Identity: id, Port: port})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint+"/v2/sandbox/forward", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token.Reveal())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "molr-tcp-v1")
	// Keep the guarded dialer and pinned TLS certificate. Only the established
	// stream loses the ordinary JSON call's 30-second timeout.
	transport := c.HTTP.Transport.(*http.Transport).Clone()
	transport.ForceAttemptHTTP2 = false
	transport.Protocols = new(http.Protocols)
	transport.Protocols.SetHTTP1(true)
	transport.ResponseHeaderTimeout = 5 * time.Second
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer transport.CloseIdleConnections()
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusSwitchingProtocols || !strings.EqualFold(res.Header.Get("Upgrade"), "molr-tcp-v1") {
		res.Body.Close()
		return nil, core.NewError(core.Unavailable, "remote forward unavailable")
	}
	stream, ok := res.Body.(io.ReadWriteCloser)
	if !ok {
		res.Body.Close()
		return nil, core.NewError(core.Unavailable, "remote stream unavailable")
	}
	return &portforward.Framed{ReadWriteCloser: stream}, nil
}
