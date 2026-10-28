package tui

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func keyboardTestWriter(t *testing.T) *terminalWriter {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "terminal")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return &terminalWriter{File: f}
}

func TestExtendedKeyboardThroughTerminalDecoder(t *testing.T) {
	for _, sequence := range []string{"\x1b[13;3u", "\x1b[13;33u", "\x1b[13;67u", "\x1b[13;2u", "\x1b[27;3;13~"} {
		w := keyboardTestWriter(t)
		w.keyboardFilter(nil, tea.WindowSizeMsg{})
		w.keyboardFilter(nil, tea.WindowSizeMsg{})
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		input := "\x1b[?0u" + "first" + sequence + "second\x1b[13;3:3u\x1b[113;5u"
		program := tea.NewProgram(newModel(), tea.WithContext(ctx), tea.WithInput(strings.NewReader(input)), tea.WithOutput(io.Discard), tea.WithFilter(w.keyboardFilter), tea.WithoutRenderer(), tea.WithoutSignalHandler())
		result, err := program.Run()
		if err != nil {
			t.Fatal(err)
		}
		m := result.(model)
		if m.composerText() != "first\nsecond" || m.assistants[0].busy {
			t.Fatalf("%q sent instead of newline: %q", sequence, m.composerText())
		}
		written, _ := os.ReadFile(w.Name())
		if string(written) != ansi.RequestKittyKeyboard+ansi.PushKittyKeyboard(ansi.KittyDisambiguateEscapeCodes) || !w.keyboardEnabled {
			t.Fatalf("negotiation: %q", written)
		}
		const leave = "\x1b[?1049l"
		if n, err := w.Write([]byte(leave)); err != nil || n != len(leave) {
			t.Fatal("restore failed", n, err)
		}
		w.WriteString(leave)
		written, _ = os.ReadFile(w.Name())
		if !strings.HasSuffix(string(written), ansi.PopKittyKeyboard(1)+leave+leave) || w.keyboardEnabled || w.keyboardRequested {
			t.Fatalf("did not pop exactly once before leaving screen: %q", written)
		}
	}
}

func TestExtendedKeyboardShortcutsAndPasteIsolation(t *testing.T) {
	for _, tc := range []struct {
		input, want string
		sent        bool
	}{
		{"before\x1b[99;5uafter\x11", "after", false},
		{"ordinary\r\x11", "", true},
		{"\x1b[200~literal\x1b[13;3u\x1b[201~\x11", "literal[13;3u", false},
	} {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		w := keyboardTestWriter(t)
		program := tea.NewProgram(newModel(), tea.WithContext(ctx), tea.WithInput(strings.NewReader(tc.input)), tea.WithOutput(io.Discard), tea.WithFilter(w.keyboardFilter), tea.WithoutRenderer(), tea.WithoutSignalHandler())
		result, err := program.Run()
		if err != nil {
			t.Fatal(err)
		}
		m := result.(model)
		if m.composerText() != tc.want || m.assistants[0].busy != tc.sent {
			t.Fatalf("input %q: body %q, sent %v", tc.input, m.composerText(), m.assistants[0].busy)
		}
	}
	for _, seq := range []string{"\x1b[13;3:3u", "\x1b[13;9u", "\x1b[13;0u", "\x1b[1114112;3u", "\x1b[13;3:4u"} {
		if msg, handled := extendedKeyboardKey(seq); !handled || msg != nil {
			t.Fatalf("unsafe sequence became a key: %q", seq)
		}
	}
}
