package portforward

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestFramedBidirectionalHalfClose(t *testing.T) {
	a, b := net.Pipe()
	left, right := &Framed{ReadWriteCloser: a}, &Framed{ReadWriteCloser: b}
	defer left.Close()
	defer right.Close()
	a.SetDeadline(time.Now().Add(3 * time.Second))
	b.SetDeadline(time.Now().Add(3 * time.Second))
	payload := bytes.Repeat([]byte{0, 1, 2, 255}, 30000)
	done := make(chan error, 1)
	go func() {
		data, err := io.ReadAll(right)
		if err == nil && !bytes.Equal(data, payload) {
			err = errors.New("truncated request")
		}
		if err == nil {
			_, err = right.Write(data)
		}
		if err == nil {
			err = right.CloseWrite()
		}
		done <- err
	}()
	if _, err := left.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := left.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(left)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatal("half-close truncated response")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := left.Write([]byte("late")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
}

type memoryStream struct{ *bytes.Buffer }

func (memoryStream) Close() error { return nil }
func TestFramedRejectsOversizedAndTruncatedData(t *testing.T) {
	for _, tc := range []struct {
		length uint32
		body   []byte
	}{{frameLimit + 1, nil}, {100, []byte("short")}} {
		raw := binary.BigEndian.AppendUint32(nil, tc.length)
		raw = append(raw, tc.body...)
		s := &Framed{ReadWriteCloser: memoryStream{bytes.NewBuffer(raw)}}
		if _, err := io.ReadAll(s); err == nil {
			t.Fatal("accepted broken frame")
		}
	}
}
