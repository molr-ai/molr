//go:build linux && (amd64 || arm64)

package sandbox

import (
	"io"
	"net"
	"sync"
	"time"
)

// All writes reserve durable credit before forwarding. Unknown outcomes keep
// their reservation; a helper crash can never manufacture remaining credit.
type accountedConn struct {
	net.Conn
	g      *networkGateway
	target int
	slot   string
	udp    bool
	once   sync.Once
}

func (c *accountedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { c.g.exchange(networkMessage{Kind: "release", Target: c.target, Token: c.slot}) })
	return err
}
func (c *accountedConn) CloseWrite() error {
	if h, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return h.CloseWrite()
	}
	return c.Close()
}
func (c *accountedConn) reserve(n int) networkMessage {
	return c.g.exchange(networkMessage{Kind: "reserve", Target: c.target, Bytes: int64(n)})
}
func (c *accountedConn) settle(r networkMessage, n int) bool {
	return c.g.exchange(networkMessage{Kind: "settle", Target: c.target, Token: r.Token, Bytes: int64(n)}).Allowed
}
func (c *accountedConn) Read(b []byte) (int, error) {
	// Limit idle reads so revoked/expired network grants do not survive forever.
	c.Conn.SetReadDeadline(time.Now().Add(time.Second))
	idleUntil := time.Now().Add(60 * time.Second)
	for {
		if !c.g.exchange(networkMessage{Kind: "check", Target: c.target}).Allowed {
			return 0, io.ErrClosedPipe
		}
		n, err := c.Conn.Read(b[:min(len(b), 65536)])
		if n == 0 {
			if e, ok := err.(net.Error); ok && e.Timeout() {
				if c.udp && !time.Now().Before(idleUntil) {
					return 0, err
				}
				c.Conn.SetReadDeadline(time.Now().Add(time.Second))
				continue
			}
			return n, err
		}
		r := c.reserve(n)
		if !r.Allowed {
			c.g.notice("network_bytes_exhausted")
			return 0, io.ErrUnexpectedEOF
		}
		if r.End != 0 && time.Now().UnixNano() >= r.End {
			c.settle(r, 0)
			r = c.reserve(n)
			if !r.Allowed {
				return 0, io.ErrUnexpectedEOF
			}
		}
		allowed := int(r.Bytes)
		if !c.settle(r, allowed) {
			return 0, io.ErrUnexpectedEOF
		}
		if allowed < n {
			return allowed, io.ErrUnexpectedEOF
		}
		return n, err
	}
}
func (c *accountedConn) Write(b []byte) (int, error) {
	written := 0
	for written < len(b) {
		r := c.reserve(min(len(b)-written, 65536))
		if !r.Allowed {
			c.g.notice("network_bytes_exhausted")
			return written, io.ErrShortWrite
		}
		var deadline time.Time
		if r.End != 0 {
			deadline = time.Unix(0, r.End)
		}
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			if !c.settle(r, 0) {
				return written, io.ErrUnexpectedEOF
			}
			continue
		}
		if end, ok := c.g.ctx.Deadline(); ok && (deadline.IsZero() || end.Before(deadline)) {
			deadline = end
		}
		c.Conn.SetWriteDeadline(deadline)
		n, err := c.Conn.Write(b[written : written+int(r.Bytes)])
		written += n
		if !c.settle(r, n) {
			return written, io.ErrUnexpectedEOF
		}
		if err != nil {
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() && r.End != 0 && time.Now().UnixNano() >= r.End && c.g.ctx.Err() == nil {
				continue
			}
			return written, err
		}
	}
	return written, nil
}
