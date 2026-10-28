package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
)

var pastedTextStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "5", Dark: "13"})

// The temporary prompt marker carries the widget's visual row through its own
// wrapping and viewport. It is removed before anything reaches the terminal.
var composerRowMarker = regexp.MustCompile("\x1b\\]777;molr-row=([0-9]+)\x07")

type composerVisualRow struct{ start, end int }

func composerVisualRows(input textarea.Model) []composerVisualRow {
	lines := strings.Split(input.Value(), "\n")
	starts := make([]int, len(lines))
	for i := 1; i < len(lines); i++ {
		starts[i] = starts[i-1] + utf8.RuneCountInString(lines[i-1]) + 1
	}
	probe := input
	for probe.Line() > 0 {
		probe.CursorStart()
		probe.CursorUp()
	}
	probe.CursorStart()
	var rows []composerVisualRow
	for {
		line, info := probe.Line(), probe.LineInfo()
		start := starts[line] + info.StartColumn
		end := starts[line] + utf8.RuneCountInString(lines[line])
		probe.SetCursor(info.StartColumn)
		probe.CursorDown()
		next := probe.LineInfo()
		if probe.Line() == line && next.RowOffset <= info.RowOffset {
			rows = append(rows, composerVisualRow{start, end})
			break
		}
		if probe.Line() == line {
			end = starts[line] + next.StartColumn
		}
		rows = append(rows, composerVisualRow{start, end})
	}
	return rows
}

func (m model) composerView() string {
	if len(m.assistants) == 0 || len(m.assistants[m.selected].pastes) == 0 {
		return m.input.View()
	}
	rows := composerVisualRows(m.input)
	runes := []rune(m.input.Value())
	probe := m.input
	probe.SetPromptFunc(2, func(row int) string {
		prompt := "  "
		if row == 0 {
			prompt = "› "
		}
		return fmt.Sprintf("\x1b]777;molr-row=%d\x07%s", row, prompt)
	})
	visible := strings.Split(probe.View(), "\n")
	for i, line := range visible {
		match := composerRowMarker.FindStringSubmatch(line)
		line = composerRowMarker.ReplaceAllString(line, "")
		if len(match) == 2 {
			index, _ := strconv.Atoi(match[1])
			if index < len(rows) {
				row := rows[index]
				blocks := m.assistants[m.selected].pastes
				for j := len(blocks) - 1; j >= 0; j-- {
					block := blocks[j]
					start, end := max(row.start, block.start), min(row.end, block.end)
					if start >= end {
						continue
					}
					left := 2 + cellWidth(string(runes[row.start:start]))
					right := 2 + cellWidth(string(runes[row.start:end]))
					line = cellCut(line, 0, left) + tintPaste(cellCut(line, left, right)) + cellCut(line, right, cellWidth(line))
				}
			}
		}
		visible[i] = line
	}
	return strings.Join(visible, "\n")
}

func tintPaste(text string) string {
	color := strings.TrimSuffix(pastedTextStyle.Render(""), "\x1b[0m")
	if color == "" {
		return text
	}
	return color + selectionSGR.ReplaceAllStringFunc(text, func(sgr string) string { return sgr + color }) + "\x1b[0m"
}
