package sandbox

import (
	"net"
	"sync"
)

type forwardConn struct {
	net.Conn
	mu      sync.Mutex
	stop    func() bool
	closed  bool
	release func()
}

func (c *forwardConn) setStop(stop func() bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		stop()
	} else {
		c.stop = stop
	}
}
func (c *forwardConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.stop != nil {
		c.stop()
	}
	err := c.Conn.Close()
	c.release()
	return err
}
func (c *forwardConn) CloseWrite() error {
	if conn, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return conn.CloseWrite()
	}
	return c.Close()
}
