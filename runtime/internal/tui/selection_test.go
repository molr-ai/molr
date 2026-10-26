package tui

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestSelectionPreservesTextAttributes(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(1) // ANSI256, including when tests run without a TTY.
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	// Selection begins and ends inside emphasis and crosses resets, colors and
	// compound attributes. Check effective attributes, not just escape presence.
	line := "plain \x1b[3mitalic\x1b[0m \x1b[1;4;9;38;5;6mbold\x1b[0m \x1b[3mend\x1b[0m tail"
	s := textSelection{lines: []string{line}, anchor: textPoint{x: 8}, end: textPoint{x: 23}, right: 99}
	rendered := s.render(100, 1)
	if ansi.Strip(rendered) != ansi.Strip(line) || s.text() != "alic bold end ta" {
		t.Fatalf("selection changed content: %q / %q", ansi.Strip(rendered), s.text())
	}
	if !reflect.DeepEqual(textAttributes(rendered), textAttributes(line)) {
		t.Fatalf("selection changed emphasis: %q", rendered)
	}
}

// Track only emphasis; color parameters must not be mistaken for attributes.
func textAttributes(text string) []uint8 {
	var result []uint8
	var style uint8
	for len(text) > 0 {
		if match := selectionSGR.FindStringIndex(text); match != nil && match[0] == 0 {
			codes := strings.Split(text[2:match[1]-1], ";")
			for i := 0; i < len(codes); i++ {
				n, _ := strconv.Atoi(codes[i])
				switch n {
				case 0:
					style = 0
				case 1:
					style |= 1
				case 3:
					style |= 2
				case 4:
					style |= 4
				case 9:
					style |= 8
				case 22:
					style &^= 1
				case 23:
					style &^= 2
				case 24:
					style &^= 4
				case 29:
					style &^= 8
				case 38, 48:
					if i+1 < len(codes) && codes[i+1] == "5" {
						i += 2
					} else {
						i += 4
					}
				}
			}
			text = text[match[1]:]
		} else {
			result = append(result, style)
			text = text[1:]
		}
	}
	return result
}

func TestComposerAndMessagesHaveSeparateSelectionBounds(t *testing.T) {
	for _, draft := range []string{"草稿甲乙", "第一行\n第二行\n第三行", strings.Repeat("折行文本", 45), "隐藏一\n隐藏二\n可见三\n可见四\n可见五"} {
		t.Run(draft[:6], func(t *testing.T) {
			m := newModel()
			m.input.SetValue(draft)
			m.resizeComposer()
			m.input, _ = m.input.Update(tea.KeyMsg{Type: tea.KeyCtrlEnd})
			cursor := m.input.LineInfo()
			mouse := func(action tea.MouseAction, x, y int) {
				next, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: action, X: x, Y: y})
				m = next.(model)
			}
			top := 3 + m.viewport.Height + 4
			bottom := top + m.input.Height() - 1
			// Drag from the composer beyond its bottom and right edges.
			mouse(tea.MouseActionPress, 4, top)
			mouse(tea.MouseActionMotion, m.width+20, m.height+20)
			mouse(tea.MouseActionRelease, m.width+20, m.height+20)
			if m.selection == nil || m.selection.top != top || m.selection.bottom != bottom || m.selection.end != (textPoint{x: m.width - 3, y: bottom}) {
				t.Fatal("composer selection escaped its text region")
			}
			var visible []string
			for _, line := range strings.Split(m.input.View(), "\n") {
				visible = append(visible, strings.TrimRight(ansi.Strip(ansi.Cut(line, 2, m.contentWidth())), " "))
			}
			want := strings.Join(visible, "\n")
			if m.selection.text() != want || strings.Contains(m.selection.text(), "›") {
				t.Fatalf("copy includes prompt, padding or another region: got %q want %q", m.selection.text(), want)
			}
			copied := ""
			m.writeClipboard = func(value string) error { copied = value; return nil }
			_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
			if result := cmd().(clipboardResult); result.err != nil || copied != want {
				t.Fatal("composer copy failed")
			}
			// Reverse drag up through the message list stays in the composer.
			mouse(tea.MouseActionPress, m.width-3, bottom)
			mouse(tea.MouseActionMotion, -10, 0)
			mouse(tea.MouseActionRelease, -10, 0)
			if m.selection == nil || m.selection.end != (textPoint{x: 4, y: top}) || m.selection.text() != want {
				t.Fatal("reverse composer selection escaped")
			}
			m = press(m, tea.KeyEsc)
			if m.input.Value() != draft || !m.input.Focused() || m.input.LineInfo() != cursor {
				t.Fatal("selection changed draft or caret")
			}
			// Starting in messages never captures the input below them.
			mouse(tea.MouseActionPress, 0, 3)
			mouse(tea.MouseActionMotion, m.width+20, m.height+20)
			mouse(tea.MouseActionRelease, m.width+20, m.height+20)
			if m.selection == nil || m.selection.bottom != 3+m.viewport.Height-1 || m.selection.end.y >= top {
				t.Fatal("message selection entered composer")
			}
		})
	}
}

func TestComposerPlaceholderCannotBeSelected(t *testing.T) {
	m := newModel()
	top := 3 + m.viewport.Height + 4
	next, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 4, Y: top})
	if next.(model).selection != nil {
		t.Fatal("placeholder became selectable text")
	}
}
