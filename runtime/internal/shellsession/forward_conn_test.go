package shellsession

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

type halfCloseStream struct{ io.ReadWriteCloser }

func (s halfCloseStream) CloseWrite() error { return nil }
func TestForwardStreamDeadlineAndClose(t *testing.T) {
	failure := errors.New("dial failed")
	if _, err := NetConn(nil, failure); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	client, peer := net.Pipe()
	defer peer.Close()
	conn, err := NetConn(halfCloseStream{client}, nil)
	need(t, err)
	defer conn.Close()
	need(t, conn.SetReadDeadline(time.Now().Add(20*time.Millisecond)))
	done := make(chan error, 1)
	go func() { _, err := conn.Read(make([]byte, 1)); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("deadline did not close stream")
		}
	case <-time.After(time.Second):
		t.Fatal("deadline leaked blocked reader")
	}
	need(t, conn.Close())
	need(t, conn.Close())
}
