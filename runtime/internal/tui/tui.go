// Package tui implements the terminal client and offline preview.
package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	osc52 "github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"

	"molr.ai/runtime/internal/runtimeclient"
)

// Run displays demo data only. Exiting discards all messages and simulated decisions.
func Run(ctx context.Context, input io.Reader, output io.Writer) error {
	return runModel(ctx, input, output, newModel())
}

// RunConnected displays authenticated Runtime data using the same terminal UI.
func RunConnected(ctx context.Context, input io.Reader, output io.Writer, client *runtimeclient.Client) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	return runModel(ctx, input, output, newConnectedModel(ctx, client, watchClient(ctx, client)))
}

func watchClient(ctx context.Context, client *runtimeclient.Client) <-chan runtimeclient.Event {
	events := make(chan runtimeclient.Event, 1)
	go client.Watch(ctx, func(event runtimeclient.Event) {
		select {
		case events <- event:
		default:
			select {
			case <-events:
			default:
			}
			select {
			case events <- event:
			case <-ctx.Done():
			}
		}
	})
	return events
}

// RunManaged keeps connection management, login and chat in one terminal session.
func RunManaged(ctx context.Context, input io.Reader, output io.Writer, options ConnectionOptions) error {
	if options.Store == nil {
		return errors.New("connection store is required")
	}
	terminal, err := terminalOutput(input, output)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := newConnectionModel(ctx, options, clipboardWriter(terminal))
	defer func() {
		for _, s := range m.draftStores {
			s.close()
		}
	}()
	return runProgram(ctx, input, terminal, m)
}

func terminalOutput(input io.Reader, output io.Writer) (*terminalWriter, error) {
	in, inOK := input.(*os.File)
	out, outOK := output.(*os.File)
	if !inOK || !outOK || !term.IsTerminal(in.Fd()) || !term.IsTerminal(out.Fd()) {
		return nil, fmt.Errorf("molr tui requires an interactive terminal on stdin and stdout")
	}
	return &terminalWriter{File: out}, nil
}
func clipboardWriter(terminal *terminalWriter) func(string) error {
	return func(value string) error {
		sequence := osc52.New(value)
		if os.Getenv("TMUX") != "" {
			sequence = sequence.Tmux()
		} else if strings.HasPrefix(os.Getenv("TERM"), "screen") {
			sequence = sequence.Screen()
		}
		_, err := io.WriteString(terminal, sequence.String())
		return err
	}
}
func runModel(ctx context.Context, input io.Reader, output io.Writer, m model) error {
	terminal, err := terminalOutput(input, output)
	if err != nil {
		return err
	}
	m.writeClipboard = clipboardWriter(terminal)
	return runProgram(ctx, input, terminal, m)
}
func runProgram(ctx context.Context, input io.Reader, terminal *terminalWriter, m tea.Model) error {
	framed, closeInput, err := frameTerminalInput(ctx, input)
	if err != nil {
		return err
	}
	defer closeInput()
	input = terminalInput{File: input.(term.File), framed: framed}
	_, err = tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(terminal), tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithFilter(terminal.keyboardFilter)).Run()
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil
	}
	return err
}

// Keep clipboard control sequences and screen frames from interleaving.
type terminalWriter struct {
	*os.File
	mu                                 sync.Mutex
	keyboardRequested, keyboardEnabled bool
}

func (w *terminalWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if bytes.HasPrefix(data, []byte(ansi.CursorHomePosition)) && bytes.ContainsRune(data, '\n') {
		// A frame uses absolute screen rows. Terminal-specific glyph widths
		// must not make a line wrap and displace subsequent cached rows.
		if _, err := w.File.Write(anchorTerminalFrame(data)); err != nil {
			return 0, err
		}
		return len(data), nil
	}
	// Kitty modes are per screen. Pop while still on the alternate screen,
	// before Bubble Tea restores the shell (including signal/error exits).
	const leaveAlt = "\x1b[?1049l"
	if bytes.Contains(data, []byte(leaveAlt)) {
		w.keyboardRequested = false
		if w.keyboardEnabled {
			w.keyboardEnabled = false
			restored := bytes.Replace(data, []byte(leaveAlt), []byte(ansi.PopKittyKeyboard(1)+leaveAlt), 1)
			if _, err := w.File.Write(restored); err != nil {
				return 0, err
			}
			return len(data), nil
		}
	}
	return w.File.Write(data)
}

func anchorTerminalFrame(data []byte) []byte {
	var out bytes.Buffer
	out.WriteString(ansi.ResetAutoWrapMode)
	for row, line := range bytes.Split(data, []byte{'\n'}) {
		if row > 0 {
			out.WriteString(ansi.CursorPosition(1, row+1))
		}
		out.Write(line)
	}
	out.WriteString(ansi.SetAutoWrapMode)
	return out.Bytes()
}

func (w *terminalWriter) WriteString(value string) (int, error) {
	return w.Write([]byte(value))
}
