// Package portforward carries bounded TCP streams, including half-closes, over
// an authenticated HTTP upgrade. It never interprets application bytes.
package portforward

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

const frameLimit = 32 << 10

type Stream interface {
	io.ReadWriteCloser
	CloseWrite() error
}

type Framed struct {
	io.ReadWriteCloser
	writeMu             sync.Mutex
	remaining           uint32
	readDone, writeDone bool
}

func (s *Framed) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if s.readDone {
		return 0, io.EOF
	}
	if s.remaining == 0 {
		var header [4]byte
		if _, err := io.ReadFull(s.ReadWriteCloser, header[:]); err != nil {
			return 0, err
		}
		s.remaining = binary.BigEndian.Uint32(header[:])
		if s.remaining > frameLimit {
			return 0, fmt.Errorf("invalid forward frame")
		}
		if s.remaining == 0 {
			s.readDone = true
			return 0, io.EOF
		}
	}
	n, err := s.ReadWriteCloser.Read(p[:min(len(p), int(s.remaining))])
	s.remaining -= uint32(n)
	if err == io.EOF && s.remaining > 0 {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}
func (s *Framed) Write(p []byte) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.writeDone {
		return 0, io.ErrClosedPipe
	}
	total := 0
	for len(p) > 0 {
		n := min(len(p), frameLimit)
		if err := s.header(uint32(n)); err != nil {
			return total, err
		}
		written, err := writeAll(s.ReadWriteCloser, p[:n])
		total += written
		if err != nil {
			return total, err
		}
		p = p[n:]
	}
	return total, nil
}
func writeAll(w io.Writer, p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		n, err := w.Write(p)
		total += n
		p = p[n:]
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}
func (s *Framed) header(n uint32) error {
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], n)
	_, err := writeAll(s.ReadWriteCloser, header[:])
	return err
}
func (s *Framed) CloseWrite() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.writeDone {
		return nil
	}
	s.writeDone = true
	return s.header(0)
}
func Bridge(a, b Stream) {
	defer a.Close()
	defer b.Close()
	done := make(chan struct{})
	copyHalf := func(dst, src Stream) {
		if _, err := io.Copy(dst, src); err != nil {
			a.Close()
			b.Close()
		} else if dst.CloseWrite() != nil {
			a.Close()
			b.Close()
		}
	}
	go func() { copyHalf(a, b); close(done) }()
	copyHalf(b, a)
	<-done
}
