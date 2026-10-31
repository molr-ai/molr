package tui

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
)

func TestFramedMouseInputThroughTerminalDecoder(t *testing.T) {
	const wheel = "\x1b[<64;57;22M"
	const literal = "[<64;57;22M"
	for _, size := range []int{1, 2, 3, 7, 13, 255, 256, 4096} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			// Fragment reads without introducing real input delays. A busy runner
			// must not turn a scheduling pause into a standalone Escape timeout.
			synctest.Test(t, func(t *testing.T) {
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
				initial := newModel()
				// This decoder test does not render or need background cursor timers.
				initial.input.Cursor.SetMode(cursor.CursorStatic)
				program := tea.NewProgram(initial, tea.WithInput(struct{ io.Reader }{terminalInput{framed: input}}), tea.WithContext(ctx), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler(), tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg {
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
		})
	}
}

func TestFramedInputEscapeTimeoutAndCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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
		synctest.Wait()
		time.Sleep(49 * time.Millisecond)
		select {
		case err := <-done:
			t.Fatalf("Escape returned before its timeout: %v", err)
		default:
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatal("Escape blocked")
		}
		go func() { b := make([]byte, 16); _, err := input.Read(b); done <- err }()
		closeInput()
		synctest.Wait()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("read survived cancellation")
			}
		default:
			t.Fatal("read blocked after cancellation")
		}
	})
}

func TestFramedInputEscapeContinuationBeforeTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source, writer := io.Pipe()
		defer source.Close()
		defer writer.Close()
		input, closeInput, err := frameTerminalInput(t.Context(), source)
		if err != nil {
			t.Fatal(err)
		}
		defer closeInput()
		go func() {
			_, _ = writer.Write([]byte{0x1b})
			time.Sleep(49 * time.Millisecond)
			_, _ = writer.Write([]byte("[<64;57;22M"))
		}()
		buffer := make([]byte, 256)
		n, err := input.Read(buffer)
		if err != nil || string(buffer[:n]) != "\x1b[<64;57;22M" {
			t.Fatalf("mouse sequence: got %q, error %v", buffer[:n], err)
		}
	})
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

func TestFramedKeyboardMessagesOwnBytesAndPreserveOrder(t *testing.T) {
	input, closeInput, err := frameTerminalInput(t.Context(), strings.NewReader("a\x1b[?0ub\x1b[13;3uc\x1b[200~\x1b[13;3u\x1b[201~d"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeInput()
	var got []string
	var messages []keyboardSequenceMsg
	input.send = func(msg tea.Msg) {
		sequence := msg.(keyboardSequenceMsg)
		messages = append(messages, sequence)
		got = append(got, "key:"+string(sequence))
	}
	buffer := make([]byte, 256)
	for {
		n, err := input.Read(buffer)
		if n > 0 {
			got = append(got, string(buffer[:n]))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"a", "key:\x1b[?0u", "b", "key:\x1b[13;3u", "c", "\x1b[200~", "\x1b[13;3u", "\x1b[201~", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("input order/paste: got %q, want %q", got, want)
	}
	if len(messages) != 2 || messages[0] != "\x1b[?0u" || messages[1] != "\x1b[13;3u" {
		t.Fatalf("messages changed after buffer reuse: %q", messages)
	}
}
