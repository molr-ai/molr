package tui

import (
	"context"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
	"github.com/muesli/cancelreader"
)

// Retain terminal identity for Bubble Tea's raw-mode setup and restoration,
// without exposing Name: polling the original fd would miss buffered events.
type terminalInput struct {
	term.File
	framed io.Reader
}

func (r terminalInput) Read(p []byte) (int, error) { return r.framed.Read(p) }

// Bubble Tea v1's unknown CSI messages alias its reusable read buffer. Decode
// keyboard sequences before that buffer is reused and send owned strings from
// the same input reader, preserving ordering with ordinary keys and mouse input.
type framedInput struct {
	io.ReadCloser
	send  func(tea.Msg)
	paste bool
}
type keyboardSequenceMsg string

func (r *framedInput) Read(p []byte) (int, error) {
	for {
		n, err := r.ReadCloser.Read(p)
		if err != nil || r.send == nil {
			return n, err
		}
		sequence := string(p[:n])
		switch sequence {
		case "\x1b[200~":
			r.paste = true
		case "\x1b[201~":
			r.paste = false
		default:
			if !r.paste && strings.HasPrefix(sequence, "\x1b[") && (strings.HasSuffix(sequence, "u") || strings.HasPrefix(sequence, "\x1b[27;") && strings.HasSuffix(sequence, "~")) {
				r.send(keyboardSequenceMsg(sequence))
				continue
			}
		}
		return n, err
	}
}

// Bubble Tea v1 treats short reads as event boundaries and can decode a partial
// mouse CSI as text. Pipe writes keep each CSI within its 256-byte input buffer.
func frameTerminalInput(ctx context.Context, input io.Reader) (*framedInput, func(), error) {
	source, err := cancelreader.NewReader(input)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	reader, writer := io.Pipe()
	type chunk struct {
		data []byte
		err  error
	}
	chunks := make(chan chunk)
	go func() {
		defer source.Close()
		for {
			data := make([]byte, 4096)
			n, err := source.Read(data)
			select {
			case chunks <- chunk{data[:n], err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		defer writer.Close()
		var pending []byte
		for {
			// Only a lone Esc needs a timeout. A recognized CSI or UTF-8
			// prefix must survive delayed continuation (for example over SSH).
			var timeout <-chan time.Time
			var timer *time.Timer
			if len(pending) == 1 && pending[0] == 0x1b {
				timer = time.NewTimer(50 * time.Millisecond)
				timeout = timer.C
			}
			select {
			case <-ctx.Done():
				if timer != nil {
					timer.Stop()
				}
				return
			case c := <-chunks:
				if timer != nil {
					timer.Stop()
				}
				pending = append(pending, c.data...)
				for len(pending) > 0 {
					n := terminalInputBoundary(pending)
					if n == 0 {
						break
					}
					if _, err := writer.Write(pending[:n]); err != nil {
						return
					}
					pending = pending[n:]
				}
				if c.err != nil {
					if len(pending) > 0 {
						_, _ = writer.Write(pending)
					}
					_ = writer.CloseWithError(c.err)
					return
				}
			case <-timeout:
				if _, err := writer.Write(pending); err != nil {
					return
				}
				pending = nil
			}
		}
	}()
	return &framedInput{ReadCloser: reader}, func() { cancel(); source.Cancel(); reader.Close() }, nil
}

func terminalInputBoundary(data []byte) int {
	if data[0] == 0x1b {
		if len(data) < 2 {
			return 0
		}
		if data[1] == '[' {
			for i := 2; i < len(data) && i < 256; i++ {
				if data[i] >= 0x40 && data[i] <= 0x7e {
					return i + 1
				}
			}
			if len(data) < 256 {
				return 0
			}
			return 256
		}
		if data[1] == 'O' {
			if len(data) < 3 {
				return 0
			}
			return 3
		}
		if !utf8.FullRune(data[1:]) {
			return 0
		}
		_, size := utf8.DecodeRune(data[1:])
		return 1 + size
	}
	n := 0
	for n < len(data) && data[n] != 0x1b {
		if !utf8.FullRune(data[n:]) {
			break
		}
		_, size := utf8.DecodeRune(data[n:])
		if n+size > 255 {
			break
		}
		n += size
	}
	return n
}
