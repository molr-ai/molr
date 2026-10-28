package tui

import (
	"errors"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

type textPoint struct{ x, y int }

type textSelection struct {
	id          uint64
	lines       []string
	anchor, end textPoint
	top, bottom int
	left, right int
	dragging    bool
	status      string
}

type clipboardResult struct {
	id  uint64
	err error
}

func (m *model) clearSelection() tea.Cmd {
	if m.selection == nil {
		return nil
	}
	m.selection = nil
	return m.focusPage()
}

func (m model) selectWithMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action == tea.MouseActionPress {
		// Any new click clears the previous range, including outside the text.
		cmd := m.clearSelection()
		if msg.Button != tea.MouseButtonLeft {
			return m, cmd
		}
		top, height := 3, m.bodyHeight()
		left, right := 0, m.width-1
		if m.page == chatPage {
			height = m.viewport.Height
			// The composer follows the viewport, a blank row, status, another
			// blank row and its top padding. Only its text rows are selectable.
			inputTop := top + height + 4 + m.attachmentRows()
			if msg.Y >= inputTop && msg.Y < inputTop+m.input.Height() && m.input.Value() != "" {
				top, height = inputTop, m.input.Height()
				left, right = 4, m.width-3 // Exclude outer padding and the prompt.
			}
		}
		if m.width < 42 || m.height < 18 || msg.Y < top || msg.Y >= top+height || msg.X < left || msg.X > right {
			return m, cmd
		}
		m.input.Blur()
		m.search.Blur()
		m.selectionID++
		point := textPoint{x: msg.X, y: msg.Y}
		m.selection = &textSelection{id: m.selectionID, lines: strings.Split(m.View(), "\n"), anchor: point, end: point, top: top, bottom: top + height - 1, left: left, right: right, dragging: true}
		return m, nil
	}
	if m.selection == nil || !m.selection.dragging {
		return m, nil
	}
	if msg.Action == tea.MouseActionMotion || msg.Action == tea.MouseActionRelease {
		m.selection.end = textPoint{x: max(m.selection.left, min(m.selection.right, msg.X)), y: max(m.selection.top, min(m.selection.bottom, msg.Y))}
	}
	if msg.Action == tea.MouseActionRelease {
		m.selection.dragging = false
		if m.selection.anchor == m.selection.end {
			cmd := m.clearSelection()
			return m, cmd
		}
		if strings.TrimSpace(m.selection.text()) != "" {
			m.selection.status = "正在复制…"
			return m, m.copySelection()
		}
	}
	return m, nil
}

func (s *textSelection) bounds() (textPoint, textPoint) {
	a, b := s.anchor, s.end
	if a.y > b.y || a.y == b.y && a.x > b.x {
		a, b = b, a
	}
	return a, b
}

// Mouse positions are terminal cells, not bytes or runes. Snap both ends to
// grapheme boundaries so Chinese text, emoji and combining marks stay intact.
func cellBoundary(line string, x int, end bool) int {
	position := 0
	g := uniseg.NewGraphemes(ansi.Strip(line))
	for g.Next() {
		width := g.Width()
		if keycapEmoji.MatchString(g.Str()) {
			width = 2
		}
		if x < position+width {
			if end {
				return position + width
			}
			return position
		}
		position += width
	}
	return position
}

func (s *textSelection) rangeForLine(y int) (int, int) {
	a, b := s.bounds()
	start, end := s.left, min(s.right+1, cellWidth(s.lines[y]))
	if y == a.y {
		start = cellBoundary(s.lines[y], a.x, false)
	}
	if y == b.y {
		end = cellBoundary(s.lines[y], b.x, true)
	}
	return start, end
}

func (s *textSelection) text() string {
	if s.anchor == s.end {
		return ""
	}
	a, b := s.bounds()
	var lines []string
	for y := a.y; y <= b.y && y < len(s.lines); y++ {
		start, end := s.rangeForLine(y)
		lines = append(lines, strings.TrimRight(cellCut(ansi.Strip(s.lines[y]), start, end), " "))
	}
	return strings.Join(lines, "\n")
}

func (s *textSelection) render(width, height int) string {
	lines := append([]string(nil), s.lines...)
	if s.anchor == s.end {
		return strings.Join(lines, "\n")
	}
	if s.anchor != s.end {
		a, b := s.bounds()
		for y := a.y; y <= b.y && y < len(lines); y++ {
			start, end := s.rangeForLine(y)
			line := lines[y]
			selected := cellCut(line, start, end)
			lines[y] = cellCut(line, 0, start) + highlightSelection(selected) + cellCut(line, end, cellWidth(line))
		}
	}
	if height >= 2 && height-2 < len(lines) {
		left := "松开鼠标自动复制 · Ctrl+C 再次复制"
		if s.status != "" {
			left = s.status
		}
		lines[height-2] = lipgloss.NewStyle().Width(width).Padding(0, 2).Render(muted.Render(spread(left, "单击 / Esc 取消", max(1, width-4))))
	}
	return strings.Join(lines, "\n")
}

var selectionSGR = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

func highlightSelection(text string) string {
	// Preserve emphasis and other attributes. Reapply only selection colors
	// after every SGR, including resets and inline foreground/background changes.
	colors := strings.TrimSuffix(selectedTextStyle.Render(""), "\x1b[0m")
	return colors + selectionSGR.ReplaceAllStringFunc(text, func(sgr string) string {
		return sgr + colors
	}) + "\x1b[0m"
}

func (m model) copySelection() tea.Cmd {
	value := m.selection.text()
	id := m.selection.id
	write := m.writeClipboard
	return func() tea.Msg {
		if strings.TrimSpace(value) == "" {
			return clipboardResult{id: id, err: errors.New("empty selection")}
		}
		if write == nil {
			return clipboardResult{id: id, err: errors.New("clipboard unavailable")}
		}
		return clipboardResult{id: id, err: write(value)}
	}
}
