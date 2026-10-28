package tui

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTerminalFramesAnchorChangedAndSkippedRows(t *testing.T) {
	w := keyboardTestWriter(t)
	// A changed line followed by an unchanged cached row must not inherit a
	// terminal's wrap position. The final CUP from Bubble Tea is retained.
	frame := ansi.CursorHomePosition + "\n" + strings.Repeat("☀️中", 40) + "\r\n\ninput\r\nfooter" + ansi.CursorPosition(1, 6)
	n, err := w.Write([]byte(frame))
	if err != nil || n != len(frame) {
		t.Fatal(n, err)
	}
	got, err := os.ReadFile(w.Name())
	if err != nil {
		t.Fatal(err)
	}
	want := ansi.ResetAutoWrapMode + ansi.CursorHomePosition + ansi.CursorPosition(1, 2) + strings.Repeat("☀️中", 40) + "\r" + ansi.CursorPosition(1, 3) + ansi.CursorPosition(1, 4) + "input\r" + ansi.CursorPosition(1, 5) + "footer" + ansi.CursorPosition(1, 6) + ansi.SetAutoWrapMode
	if string(got) != want {
		t.Fatalf("frame positions: %q", got)
	}
	if bytes.ContainsRune(got, '\n') {
		t.Fatal("frame still relies on relative line feeds")
	}
	for _, sequence := range []string{ansi.CursorHomePosition, "\x1b]52;c;YWJj\x07", "\x1b[?1049l"} {
		before := len(got)
		n, err := w.Write([]byte(sequence))
		if err != nil || n != len(sequence) {
			t.Fatal(n, err)
		}
		got, _ = os.ReadFile(w.Name())
		if string(got[before:]) != sequence {
			t.Fatal("non-frame terminal control was changed")
		}
	}
}
