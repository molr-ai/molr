package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestPasteColorPreservesWrappingViewportAndDraft(t *testing.T) {
	profile, dark := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
	lipgloss.SetColorProfile(1)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile); lipgloss.SetHasDarkBackground(dark) })
	for _, background := range []bool{false, true} {
		lipgloss.SetHasDarkBackground(background)
		for _, width := range []int{16, 35, 80} {
			m := newModel()
			m.input.SetWidth(width)
			m = typeText(m, "before ")
			m = pasteText(m, strings.Repeat("中文👩‍💻é ", 25))
			m = typeText(m, " after\nrow2\nrow3\nrow4\nrow5\nrow6")
			for _, key := range []tea.KeyType{tea.KeyCtrlHome, tea.KeyCtrlEnd} {
				m = press(m, key)
				value, caret, original := m.input.Value(), m.input.LineInfo(), m.input.View()
				rendered := m.composerView()
				if ansi.Strip(rendered) != ansi.Strip(original) || strings.Contains(rendered, "molr-row") {
					t.Fatalf("width %d: changed layout\ngot %q\nwant %q", width, rendered, original)
				}
				if m.input.Value() != value || m.input.LineInfo() != caret || ansi.Strip(m.input.View()) != ansi.Strip(original) {
					t.Fatal("render changed draft, caret or viewport")
				}
				color := strings.TrimSuffix(pastedTextStyle.Render(""), "\x1b[0m")
				if color == "" || key == tea.KeyCtrlHome && !strings.Contains(rendered, color) {
					t.Fatalf("paste color missing: %q", rendered)
				}
			}
		}
	}
}

func TestPasteColorDoesNotTintAdjacentOrdinaryText(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(1)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	m := newModel()
	m = typeText(m, "before ")
	m = pasteText(m, strings.Repeat("x", 101))
	m = typeText(m, " after")
	m.input.Blur()
	view := m.composerView()
	label := m.assistants[0].pastes[0]
	color := strings.TrimSuffix(pastedTextStyle.Render(""), "\x1b[0m")
	position, tinted := 0, false
	for len(view) > 0 {
		if match := selectionSGR.FindStringIndex(view); match != nil && match[0] == 0 {
			sgr := view[:match[1]]
			if sgr == color {
				tinted = true
			} else if sgr == "\x1b[0m" {
				tinted = false
			}
			view = view[match[1]:]
			continue
		}
		_, size := utf8.DecodeRuneInString(view)
		if tinted != (position >= 2+label.start && position < 2+label.end) {
			t.Fatalf("wrong color at rune %d", position)
		}
		position++
		view = view[size:]
	}
}
