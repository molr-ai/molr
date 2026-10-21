package sandbox

import (
	"bytes"
	"context"
	"io"
	"molr.ai/runtime/internal/platform"
	"net"
	"os"
	"sync"
)

type stream struct {
	mu      sync.Mutex
	b       bytes.Buffer
	done    bool
	changed chan struct{}
	budget  *outputBudget
}
type outputBudget struct {
	mu        sync.Mutex
	left      int64
	truncated bool
}

func newStream(b *outputBudget) *stream { return &stream{changed: make(chan struct{}), budget: b} }
func (s *stream) Write(data []byte) (int, error) {
	n := len(data)
	s.budget.mu.Lock()
	k := int64(n)
	if k > s.budget.left {
		k = s.budget.left
		s.budget.truncated = true
	}
	s.budget.left -= k
	s.budget.mu.Unlock()
	s.mu.Lock()
	if k > 0 {
		s.b.Write(data[:k])
		close(s.changed)
		s.changed = make(chan struct{})
	}
	s.mu.Unlock()
	return n, nil
}
func (s *stream) Read(data []byte) (int, error) {
	for {
		s.mu.Lock()
		if s.b.Len() > 0 {
			n, e := s.b.Read(data)
			s.mu.Unlock()
			return n, e
		}
		if s.done {
			s.mu.Unlock()
			return 0, io.EOF
		}
		ch := s.changed
		s.mu.Unlock()
		<-ch
	}
}
func (s *stream) finish() { s.mu.Lock(); s.done = true; close(s.changed); s.mu.Unlock() }

type process struct {
	fullAccess        bool
	networkDiagnostic func() platform.NetworkDiagnostic
	forward           func(context.Context, int) (net.Conn, error)
	terminalControl   func(context.Context, string, uint16, uint16) error
	terminal          *os.File
	stdout, stderr    *stream
	done              chan struct{}
	cancel            context.CancelFunc
	exit              platform.Exit
	err               error
	operation         string
}

func (p *process) Stdout() io.Reader { return p.stdout }
func (p *process) Stderr() io.Reader { return p.stderr }
func (p *process) Wait(ctx context.Context) (platform.Exit, error) {
	select {
	case <-p.done:
		return p.exit, p.err
	case <-ctx.Done():
		return platform.Exit{}, ctx.Err()
	}
}
func (p *process) Cancel(ctx context.Context) error {
	p.cancel()
	select {
	case <-p.done:
		return p.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *process) NetworkDiagnostic() platform.NetworkDiagnostic {
	if p.networkDiagnostic != nil {
		return p.networkDiagnostic()
	}
	return platform.NetworkDiagnostic{}
}
