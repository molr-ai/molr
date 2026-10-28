package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestFramedMouseInputThroughTerminalDecoder(t *testing.T) {
	const wheel = "\x1b[<64;57;22M"
	const literal = "[<64;57;22M"
	for _, size := range []int{1, 2, 3, 7, 13, 255, 256, 4096} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			text := "草稿" + strings.Repeat(wheel, 400) + "\x1b[200~" + literal + "\x1b[201~\x11"
			var parts []io.Reader
			for len(text) > 0 {
				n := min(size, len(text))
				parts = append(parts, strings.NewReader(text[:n]))
				text = text[n:]
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			input, closeInput, err := frameTerminalInput(ctx, io.MultiReader(parts...))
			if err != nil {
				t.Fatal(err)
			}
			defer closeInput()
			count := 0
			program := tea.NewProgram(newModel(), tea.WithInput(struct{ io.Reader }{terminalInput{framed: input}}), tea.WithContext(ctx), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler(), tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg {
				if mouse, ok := msg.(tea.MouseMsg); ok {
					if mouse.Button != tea.MouseButtonWheelUp || mouse.X != 56 || mouse.Y != 21 {
						t.Errorf("wrong mouse event: %+v", mouse)
					}
					count++
				}
				return msg
			}))
			result, err := program.Run()
			if err != nil {
				t.Fatal(err)
			}
			m := result.(model)
			if count != 400 || m.composerText() != "草稿"+literal {
				t.Fatalf("mouse events=%d; draft=%q", count, m.composerText())
			}
		})
	}
}

func TestFramedInputEscapeTimeoutAndCancellation(t *testing.T) {
	source, writer := io.Pipe()
	defer source.Close()
	defer writer.Close()
	input, closeInput, err := frameTerminalInput(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer closeInput()
	go func() { _, _ = writer.Write([]byte{0x1b}) }()
	done := make(chan error, 1)
	go func() {
		b := make([]byte, 16)
		n, err := input.Read(b)
		if err == nil && (n != 1 || b[0] != 0x1b) {
			err = fmt.Errorf("lost standalone escape")
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Escape blocked")
	}
	go func() { b := make([]byte, 16); _, err := input.Read(b); done <- err }()
	closeInput()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("read survived cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("read blocked after cancellation")
	}
}

func TestFramedInputDelayedMouseAndSS3(t *testing.T) {
	source, writer := io.Pipe()
	defer source.Close()
	defer writer.Close()
	input, closeInput, err := frameTerminalInput(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer closeInput()
	go func() {
		writer.Write([]byte("\x1b[<64;"))
		time.Sleep(100 * time.Millisecond)
		writer.Write([]byte("57;22M"))
		writer.Write([]byte("\x1bO"))
		writer.Write([]byte("A"))
	}()
	for _, want := range []string{"\x1b[<64;57;22M", "\x1bOA"} {
		b := make([]byte, 256)
		n, err := input.Read(b)
		if err != nil || string(b[:n]) != want {
			t.Fatalf("input boundary: got %q, error %v", b[:n], err)
		}
	}
}
