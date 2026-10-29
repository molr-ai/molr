package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

type composerState struct {
	messageDraft                 *textarea.Model
	messagePastes                []pasteBlock
	pastes                       []pasteBlock
	clearVersion                 uint64
	attachments                  []draftAttachment
	commandBusy                  bool
	commandSignature, commandKey string
	pending                      pendingSend
	composer                     textarea.Model
}

// Message state remains available while the active editor accepts commands.
func (s composerState) messageEditor() (textarea.Model, []pasteBlock) {
	if s.messageDraft != nil {
		return *s.messageDraft, s.messagePastes
	}
	return s.composer, s.pastes
}

func (s *composerState) acknowledgeMessage(text string, version uint64) bool {
	editor, pastes := s.messageEditor()
	if s.clearVersion != version || strings.TrimSpace(expandPasteBlocks(editor.Value(), pastes)) != text {
		return false
	}
	if s.messageDraft != nil {
		s.messageDraft.Reset()
		s.messagePastes = nil
	} else {
		s.composer.Reset()
		s.pastes = nil
	}
	return true
}

// The selected textarea is the active editor; inactive editors belong to their assistant.
func (m *model) saveComposer() {
	if len(m.assistants) > 0 {
		m.assistants[m.selected].composer = m.input
	}
}

func (m *model) activateComposer() {
	m.input = m.assistants[m.selected].composer
	m.input.SetWidth(m.contentWidth())
	m.syncComposerMode()
	m.resizeComposer()
}

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
	// Refresh the widget's content before positioning the caret: its viewport
	// may still contain the previous frame when a sixth line is inserted.
	if m.input.Focused() {
		_ = m.input.View()
		m.input, _ = m.input.Update(nil)
	}
	m.viewport.Height = max(1, m.bodyHeight()-rows-5-m.attachmentRows())
	if follow {
		m.viewport.GotoBottom()
	} else {
		m.viewport.SetYOffset(m.viewport.YOffset)
	}
}

func (m model) inputRows() int {
	if m.input.LineCount() >= 5 {
		return 5
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
		if rows >= 5 {
			return 5
		}
		if i+1 < probe.LineCount() {
			probe.CursorEnd()
			probe.CursorDown()
		}
	}
	return max(1, rows)
}

func (m model) attachmentRows() int {
	if len(m.assistants) == 0 || m.commandOnly() {
		return 0
	}
	return min(3, max(1, m.bodyHeight()-m.input.Height()-6), len(m.assistants[m.selected].attachments))
}

// Keep the message draft intact while the channel only permits commands.
func (m *model) syncComposerMode() {
	if len(m.assistants) == 0 {
		return
	}
	a := &m.assistants[m.selected]
	commands := m.channelNotice() != ""
	if commands == (a.messageDraft != nil) {
		return
	}
	focused := m.input.Focused()
	if commands {
		draft := m.input
		a.messageDraft, a.messagePastes = &draft, a.pastes
		a.pastes = nil
		m.input = newComposer()
		m.input.Placeholder = "输入命令"
		m.input.SetPromptFunc(2, func(line int) string {
			if line == 0 {
				return "/ "
			}
			return "  "
		})
		m.input.KeyMap.InsertNewline.SetEnabled(false)
	} else {
		m.input, a.pastes = *a.messageDraft, a.messagePastes
		a.messageDraft, a.messagePastes = nil, nil
	}
	m.input.SetWidth(m.contentWidth())
	if focused {
		m.input.Focus()
	} else {
		m.input.Blur()
	}
	m.commandCursor, m.commandDismissed = 0, ""
}

func (m model) commandOnly() bool {
	return len(m.assistants) > 0 && m.assistants[m.selected].messageDraft != nil
}

func commandText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return "/" + strings.TrimPrefix(value, "/")
}
