package tui

import tea "github.com/charmbracelet/bubbletea"

func (m *model) resizeComposer() {
	follow := m.viewport.AtBottom()
	rows := m.inputRows()
	if rows != m.input.Height() {
		line := m.input.Line()
		position := m.input.LineInfo()
		column := position.StartColumn + position.ColumnOffset
		focused := m.input.Focused()
		m.input.SetHeight(rows)
		// Reflow the widget's scroll position after growing or shrinking, while
		// keeping the logical caret and draft unchanged.
		m.input.Focus()
		m.input, _ = m.input.Update(tea.KeyMsg{Type: tea.KeyCtrlHome})
		for m.input.Line() < line {
			m.input.CursorEnd()
			m.input.CursorDown()
		}
		m.input.SetCursor(column)
		m.input, _ = m.input.Update(nil)
		if !focused {
			m.input.Blur()
		}
	}
	m.viewport.Height = max(1, m.bodyHeight()-rows-5)
	if follow {
		m.viewport.GotoBottom()
	} else {
		m.viewport.SetYOffset(m.viewport.YOffset)
	}
}

func (m model) inputRows() int {
	if m.input.LineCount() >= 3 {
		return 3
	}
	// Ask the widget for its own wrapping metrics on a copy, rather than
	// implementing a second word-wrapping algorithm or moving the real caret.
	probe := m.input
	for probe.Line() > 0 {
		probe.CursorStart()
		probe.CursorUp()
	}
	rows := 0
	for i := 0; i < probe.LineCount(); i++ {
		rows += probe.LineInfo().Height
		if rows >= 3 {
			return 3
		}
		if i+1 < probe.LineCount() {
			probe.CursorEnd()
			probe.CursorDown()
		}
	}
	return max(1, rows)
}
