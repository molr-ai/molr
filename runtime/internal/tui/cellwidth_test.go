package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Independently account for the known mismatch in the pinned width library.
func displayedWidth(text string) int {
	return ansi.StringWidth(text) + len(keycapEmoji.FindAllString(text, -1))
}

func TestKeycapHistoryStaysWithinTerminalWhileScrolling(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(1)
	defer lipgloss.SetColorProfile(profile)
	for _, width := range []int{42, 80, 120} {
		m := newModel()
		m.resize(width, 24)
		m.assistants[0].approval = ""
		source := "**1️⃣ 标题** — " + strings.Repeat("中文内容 ", 25) + "\n\n" +
			"[链接](https://example.invalid/" + strings.Repeat("segment/", 20) + ")\n\n" +
			"```text\n" + strings.Repeat("2️⃣ ", width) + "\n```\n\n" +
			"| 标题 3️⃣ | 值 |\n| --- | --- |\n| *4️⃣ 内容* | 5️⃣ |"
		m.assistants[0].messages = []message{{role: "assistant", text: source}, {role: "user", text: source}}
		m.refresh(true)
		for offset := 0; offset < m.viewport.TotalLineCount(); offset++ {
			m.viewport.SetYOffset(offset)
			frame := m.View()
			lines := strings.Split(frame, "\n")
			if len(lines) != m.height {
				t.Fatalf("width %d offset %d: frame grew to %d rows", width, offset, len(lines))
			}
			for y, line := range lines {
				if displayedWidth(line) > width {
					t.Fatalf("width %d offset %d row %d: rendered %d cells", width, offset, y, displayedWidth(line))
				}
			}
			if strings.ContainsAny(frame, "㐀㐁㐂㐃㐄") {
				t.Fatal("layout placeholder reached terminal")
			}
		}
		history := m.restoreHistory(m.viewport.View())
		if !strings.Contains(ansi.Strip(history), "5️⃣") {
			t.Fatal("emoji lost from rendered history")
		}
	}
}

func TestKeycapSelectionUsesTwoCellsAndCopiesOriginal(t *testing.T) {
	line := "  前1️⃣后 #️⃣ *⃣ 㐀㐁"
	masked, restore := layoutKeycaps(line)
	if restore(masked) != line || strings.Contains(masked, "1️⃣") || !strings.Contains(masked, "㐀㐁") {
		t.Fatal("layout replacement collided with existing content")
	}
	if cellWidth("1️⃣#️⃣*⃣") != 6 {
		t.Fatal("keycap widths")
	}
	s := textSelection{lines: []string{line}, anchor: textPoint{x: 5, y: 0}, end: textPoint{x: 6, y: 0}, right: 99}
	if s.text() != "1️⃣后" {
		t.Fatalf("copy split a keycap: %q", s.text())
	}
	// Starting in the second cell still selects the entire grapheme.
	if cellBoundary(line, 5, false) != 4 || cellBoundary(line, 5, true) != 6 {
		t.Fatal("keycap mouse coordinates")
	}
	rendered := s.render(40, 1)
	if ansi.Strip(rendered) != line {
		t.Fatal("selection changed original text")
	}
}
