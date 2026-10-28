package tui

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// Offsets address runes in the displayed composer. Original text is never
// recovered by matching labels: identical-looking pasted blocks stay distinct.
type pasteBlock struct {
	start, end  int
	label, text string
}

func expandPasteBlocks(display string, blocks []pasteBlock) string {
	runes := []rune(display)
	var out strings.Builder
	position := 0
	for _, block := range blocks {
		if block.start < position || block.end > len(runes) || string(runes[block.start:block.end]) != block.label {
			continue
		}
		out.WriteString(string(runes[position:block.start]))
		out.WriteString(block.text)
		position = block.end
	}
	out.WriteString(string(runes[position:]))
	return out.String()
}

func (m model) composerText() string {
	if m.commandOnly() {
		return commandText(m.input.Value())
	}
	if len(m.assistants) == 0 {
		return m.input.Value()
	}
	return expandPasteBlocks(m.input.Value(), m.assistants[m.selected].pastes)
}

func composerCaret(input textarea.Model) int {
	lines := strings.Split(input.Value(), "\n")
	position := 0
	for i := 0; i < input.Line() && i < len(lines); i++ {
		position += utf8.RuneCountInString(lines[i]) + 1
	}
	info := input.LineInfo()
	return position + info.StartColumn + info.ColumnOffset
}

func (m *model) setComposerCaret(position int) {
	text := []rune(m.input.Value())
	position = max(0, min(len(text), position))
	line, column := 0, 0
	for _, r := range text[:position] {
		if r == '\n' {
			line++
			column = 0
		} else {
			column++
		}
	}
	focused := m.input.Focused()
	m.input.Focus()
	m.input, _ = m.input.Update(tea.KeyMsg{Type: tea.KeyCtrlHome})
	for m.input.Line() < line {
		m.input.CursorEnd()
		m.input.CursorDown()
	}
	m.input.SetCursor(column)
	if !focused {
		m.input.Blur()
	}
}

func (m *model) insertPasteBlock(text string) {
	a := &m.assistants[m.selected]
	position := composerCaret(m.input)
	preview := []rune(strings.Join(strings.Fields(cleanText(text)), " "))
	if len(preview) > 20 {
		preview = append(preview[:20], '…')
	}
	label := fmt.Sprintf("[粘贴了 %d 字符：%s]", utf8.RuneCountInString(text), string(preview))
	length := utf8.RuneCountInString(label)
	for i := range a.pastes {
		if a.pastes[i].start >= position {
			a.pastes[i].start += length
			a.pastes[i].end += length
		}
	}
	m.input.InsertString(label)
	a.pastes = append(a.pastes, pasteBlock{start: position, end: position + length, label: label, text: text})
	slices.SortFunc(a.pastes, func(a, b pasteBlock) int { return a.start - b.start })
	m.resizeComposer()
}

func (m *model) resetComposer() {
	m.input.Reset()
	if len(m.assistants) > 0 {
		m.assistants[m.selected].pastes = nil
	}
}

func (m *model) clearComposerDraft() {
	if m.commandOnly() {
		m.resetComposer()
		m.commandDismissed, m.commandCursor, m.notice = "", 0, ""
		m.resizeComposer()
		return
	}
	m.resetComposer()
	if len(m.assistants) > 0 {
		a := &m.assistants[m.selected]
		a.clearVersion++
		for _, attachment := range a.attachments {
			if attachment.cancel != nil {
				attachment.cancel()
			}
		}
		a.attachments = nil
		// An already submitted message still has a receipt to reconcile. Clearing
		// a draft must neither stop that request nor unlock a duplicate send.
		if !a.pending.active {
			a.pending = pendingSend{}
		}
	}
	m.commandDismissed = ""
	m.commandCursor = 0
	m.notice = "已清空输入和待发送附件"
	m.resizeComposer()
}

// Let textarea retain its editing/navigation behavior, then widen any edit
// crossing a pasted block to the whole block and keep caret stops at its edges.
func (m *model) updateComposer(event tea.Msg) tea.Cmd {
	if len(m.assistants) == 0 || len(m.assistants[m.selected].pastes) == 0 {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(event)
		return cmd
	}
	before := []rune(m.input.Value())
	previousCaret := composerCaret(m.input)
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(event)
	a := &m.assistants[m.selected]
	after := []rune(m.input.Value())
	if !slices.Equal(before, after) {
		start := 0
		for start < len(before) && start < len(after) && before[start] == after[start] {
			start++
		}
		oldEnd, newEnd := len(before), len(after)
		for oldEnd > start && newEnd > start && before[oldEnd-1] == after[newEnd-1] {
			oldEnd--
			newEnd--
		}
		// Repeated characters can make a minimal diff appear inside a block
		// even when typing/deleting at its boundary. Prefer the actual caret.
		if key, ok := event.(tea.KeyMsg); ok {
			delta := len(after) - len(before)
			candidateStart, candidateEnd, candidateNewEnd := -1, -1, -1
			if delta > 0 && !key.Alt {
				candidateStart, candidateEnd, candidateNewEnd = previousCaret, previousCaret, previousCaret+delta
			}
			if delta < 0 {
				switch key.String() {
				case "backspace", "ctrl+h", "ctrl+w", "ctrl+u", "alt+backspace":
					candidateStart, candidateEnd, candidateNewEnd = previousCaret+delta, previousCaret, previousCaret+delta
				case "delete", "ctrl+d":
					candidateStart, candidateEnd, candidateNewEnd = previousCaret, previousCaret-delta, previousCaret
				}
			}
			if candidateStart >= 0 && candidateEnd <= len(before) && candidateNewEnd <= len(after) && slices.Equal(before[:candidateStart], after[:candidateStart]) && slices.Equal(before[candidateEnd:], after[candidateNewEnd:]) {
				start, oldEnd, newEnd = candidateStart, candidateEnd, candidateNewEnd
			}
		}
		from, to := start, oldEnd
		for _, block := range a.pastes {
			if block.start < to && block.end > from || from == to && block.start < from && block.end > from {
				from = min(from, block.start)
				to = max(to, block.end)
			}
		}
		if newEnd > start && (from != start || to != oldEnd) {
			// Word case/transposition shortcuts cannot rewrite part of a label.
			m.input.SetValue(string(before))
			m.setComposerCaret(previousCaret)
			return cmd
		}
		delta := (newEnd - start) - (to - from)
		remaining := make([]pasteBlock, 0, len(a.pastes))
		for _, block := range a.pastes {
			if block.start < to && block.end > from {
				continue
			}
			if block.start >= to {
				block.start += delta
				block.end += delta
			}
			remaining = append(remaining, block)
		}
		a.pastes = remaining
		if from != start || to != oldEnd {
			text := string(before[:from]) + string(after[start:newEnd]) + string(before[to:])
			m.input.SetValue(text)
			m.setComposerCaret(from + newEnd - start)
		}
	}
	caret := composerCaret(m.input)
	for _, block := range a.pastes {
		if caret > block.start && caret < block.end {
			if caret < previousCaret {
				m.setComposerCaret(block.start)
			} else {
				m.setComposerCaret(block.end)
			}
			break
		}
	}
	return cmd
}

func pasteLineCount(text string) int {
	return strings.Count(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\n") + 1
}
