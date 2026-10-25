package shellsession

import (
	"molr.ai/runtime/internal/portforward"
	"net"
	"sync"
	"time"
)

// HTTP transports require net.Conn, while the remote framed protocol exposes
// only a half-close stream. Deadlines on that stream fail closed by closing it.
type forwardNetConn struct {
	portforward.Stream
	mu                    sync.Mutex
	readTimer, writeTimer *time.Timer
	closed                bool
}

func NetConn(s portforward.Stream, err error) (net.Conn, error) {
	if err != nil {
		return nil, err
	}
	if c, ok := s.(net.Conn); ok {
		return c, nil
	}
	return &forwardNetConn{Stream: s}, nil
}
func (c *forwardNetConn) LocalAddr() net.Addr  { return &net.TCPAddr{IP: net.IPv4zero} }
func (c *forwardNetConn) RemoteAddr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }
func (c *forwardNetConn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	if c.readTimer != nil {
		c.readTimer.Stop()
	}
	if c.writeTimer != nil {
		c.writeTimer.Stop()
	}
	c.mu.Unlock()
	return c.Stream.Close()
}
func (c *forwardNetConn) deadline(t time.Time, read bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	timer := &c.writeTimer
	if read {
		timer = &c.readTimer
	}
	if *timer != nil {
		(*timer).Stop()
	}
	if !t.IsZero() && !c.closed {
		*timer = time.AfterFunc(time.Until(t), func() { c.Close() })
	}
	return nil
}
func (c *forwardNetConn) SetReadDeadline(t time.Time) error  { return c.deadline(t, true) }
func (c *forwardNetConn) SetWriteDeadline(t time.Time) error { return c.deadline(t, false) }
func (c *forwardNetConn) SetDeadline(t time.Time) error {
	c.SetReadDeadline(t)
	return c.SetWriteDeadline(t)
}

// Bound unauthenticated and idle connections before HTTP parsing/TLS handshakes.
type forwardLimitedListener struct {
	net.Listener
	local, global chan struct{}
}

func (l *forwardLimitedListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		select {
		case l.local <- struct{}{}:
		default:
			c.Close()
			continue
		}
		select {
		case l.global <- struct{}{}:
		default:
			<-l.local
			c.Close()
			continue
		}
		return &forwardCountedConn{Conn: c, release: func() { <-l.local; <-l.global }}, nil
	}
}

type forwardCountedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *forwardCountedConn) Close() error { err := c.Conn.Close(); c.once.Do(c.release); return err }
