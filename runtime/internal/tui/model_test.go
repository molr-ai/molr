package tui

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestRunRejectsNonTerminal(t *testing.T) {
	var output bytes.Buffer
	if err := Run(context.Background(), strings.NewReader("hello"), &output); err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("expected terminal error, got %v", err)
	}
	if output.Len() != 0 {
		t.Fatal("non-terminal received escape sequences")
	}
}

func TestDraftAndDelayedReplyStayWithTheirAssistant(t *testing.T) {
	m := newModel()
	m.input.SetValue("中文草稿\n第二行")
	m.selectAssistant(1)
	m.input.SetValue("审查代码")
	next, cmd := m.send()
	m = next.(model)
	if cmd == nil || !m.assistants[1].busy || m.input.Value() != "" {
		t.Fatal("send did not start")
	}
	m.selectAssistant(0)
	if m.input.Value() != "中文草稿\n第二行" {
		t.Fatal("switch lost draft")
	}
	before := len(m.assistants[0].messages)
	next, _ = m.Update(replyMsg{assistant: 1, generation: 1, text: "代码演示回复"})
	m = next.(model)
	if len(m.assistants[0].messages) != before || m.assistants[1].busy || m.assistants[1].unreadCount != 1 {
		t.Fatal("reply was attached to wrong assistant")
	}
	if last := m.assistants[1].messages[len(m.assistants[1].messages)-1]; last.text != "代码演示回复" {
		t.Fatal(last)
	}
	next, _ = m.Update(replyMsg{assistant: 1, generation: 1, text: "duplicate"})
	if len(next.(model).assistants[1].messages) != len(m.assistants[1].messages) {
		t.Fatal("duplicate response appended")
	}
}

func TestBusySendPreservesDraftAndApprovalRequiresDecision(t *testing.T) {
	m := newModel()
	m.input.SetValue("first")
	next, _ := m.send()
	m = next.(model)
	m.input.SetValue("second")
	next, cmd := m.send()
	m = next.(model)
	if cmd != nil || m.input.Value() != "second" {
		t.Fatal("busy send consumed draft")
	}
	m.openPage(approvalPage)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if m.assistants[0].approval != "pending" {
		t.Fatal("dismiss approved request")
	}
	for _, decision := range []struct {
		key    tea.KeyType
		status string
	}{{tea.KeyEnter, "approved"}, {tea.KeyDown, "rejected"}} {
		m = newModel()
		m = press(m, tea.KeyCtrlP)
		if m.approvalChoice != 0 || m.page != approvalPage {
			t.Fatal("approve should be selected on entry")
		}
		if m.assistants[0].approval != "pending" || m.page != approvalPage {
			t.Fatal("opening approval page must not resolve the request")
		}
		m = press(m, decision.key)
		if decision.key != tea.KeyEnter {
			m = press(m, tea.KeyEnter)
		}
		if m.page != chatPage || m.assistants[0].approval != decision.status || !m.input.Focused() {
			t.Fatal("decision not applied")
		}
	}
}

func TestResponsiveLayoutAndUnicodeInput(t *testing.T) {
	for _, size := range [][2]int{{200, 50}, {120, 40}, {100, 30}, {88, 18}, {70, 24}, {42, 18}} {
		for _, screen := range []page{chatPage, assistantsPage, approvalPage, helpPage} {
			for _, selection := range []int{0, 2} {
				m := newModel()
				m.resize(size[0], size[1])
				if screen != chatPage {
					m.openPage(screen)
				}
				m.pickerCursor = selection
				view := m.View()
				if width, height := lipgloss.Size(view); width > size[0] || height > size[1] {
					t.Errorf("%v page=%v selection=%v: view %dx%d", size, screen, selection, width, height)
				}
				if strings.ContainsAny(view, "╭╮╰╯│─┌┐└┘") {
					t.Fatal("border returned")
				}
				if strings.ContainsRune(view, '\x00') {
					t.Fatal("NUL padding in terminal output")
				}
				if screen == chatPage && strings.Contains(view, "日常助理") {
					t.Fatal("assistant list leaked into chat")
				}
				if screen != chatPage && strings.Contains(view, "发消息，或输入") {
					t.Fatal("composer leaked into another page")
				}
				if !utf8.ValidString(view) {
					t.Fatal("invalid UTF-8")
				}
			}
		}
	}
	m := newModel()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("你好\n多行粘贴"), Paste: true})
	m = next.(model)
	if m.input.Value() != "你好\n多行粘贴" {
		t.Fatalf("paste: %q", m.input.Value())
	}
	m.input.SetValue("中文\x1b[31m\x07正文")
	next, _ = m.send()
	last := next.(model).assistants[0].messages
	if strings.ContainsAny(last[len(last)-1].text, "\x1b\x07") {
		t.Fatal("terminal controls reached history")
	}
}

func press(m model, key tea.KeyType) model {
	next, _ := m.Update(tea.KeyMsg{Type: key})
	return next.(model)
}

func typeText(m model, text string) model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
	return next.(model)
}

func TestAssistantSearchConfirmCancelAndNestedHelp(t *testing.T) {
	m := newModel()
	m.input.SetValue("中文草稿\n第二行")
	m.input.CursorUp()
	m.input.CursorStart()
	m.input.SetCursor(2)
	m.viewport.HalfPageUp()
	offset, line, column := m.viewport.YOffset, m.input.Line(), m.input.LineInfo().ColumnOffset
	m = press(m, tea.KeyCtrlK)
	if m.page != assistantsPage || m.pickerCursor != 0 || m.input.Focused() || !m.search.Focused() {
		t.Fatal("picker entry")
	}
	m = typeText(m, "开发")
	if matches := m.matches(); len(matches) != 1 || matches[0] != 1 {
		t.Fatal(matches)
	}
	m = press(m, tea.KeyF1)
	if m.page != helpPage || m.search.Focused() {
		t.Fatal("help entry")
	}
	m = typeText(m, "ignored")
	m = press(m, tea.KeyEsc)
	if m.page != assistantsPage || m.search.Value() != "开发" || !m.search.Focused() {
		t.Fatal("help did not restore picker")
	}
	m = press(m, tea.KeyEsc)
	if m.page != chatPage || m.selected != 0 || m.input.Value() != "中文草稿\n第二行" || m.viewport.YOffset != offset || m.input.Line() != line || m.input.LineInfo().ColumnOffset != column {
		t.Fatal("cancel lost chat state")
	}
	m = press(m, tea.KeyCtrlK)
	m = typeText(m, "开发")
	m = press(m, tea.KeyEnter)
	if m.selected != 1 || m.page != chatPage || m.input.Value() != "" || !m.input.Focused() {
		t.Fatal("selection not confirmed")
	}
	m = typeText(m, "另一份草稿")
	m = press(m, tea.KeyCtrlK)
	m = typeText(m, "研究")
	m = press(m, tea.KeyEnter)
	if m.input.Value() != "中文草稿\n第二行" || m.input.Line() != line || m.input.LineInfo().ColumnOffset != column || m.viewport.YOffset != offset {
		t.Fatal("assistant switch lost draft/cursor/history")
	}
	m = typeText(m, "X")
	if m.input.Value() != "中文X草稿\n第二行" {
		t.Fatalf("cursor restoration: %q", m.input.Value())
	}
	m = press(m, tea.KeyCtrlK)
	m = typeText(m, "没有这个助理")
	m = press(m, tea.KeyEnter)
	if m.page != assistantsPage || m.selected != 0 || !strings.Contains(m.View(), "没有匹配的助理") {
		t.Fatal("empty search selected an assistant")
	}
}

func TestApprovalReturnAndBackgroundReplyPreserveHistory(t *testing.T) {
	m := newModel()
	m.resize(70, 20)
	m.assistants[0].messages = append(m.assistants[0].messages, message{role: "assistant", text: strings.Repeat("较早的记录\n", 60)})
	m.refresh(true)
	m = press(m, tea.KeyPgUp)
	m.input.SetValue("保留这份草稿")
	offset := m.viewport.YOffset
	m = press(m, tea.KeyCtrlP)
	m = press(m, tea.KeyF1)
	m = press(m, tea.KeyEsc)
	if m.page != approvalPage || m.approvalChoice != 0 {
		t.Fatal("help lost approval state")
	}
	m.assistants[0].busy, m.assistants[0].generation = true, 1
	next, _ := m.Update(replyMsg{assistant: 0, generation: 1, text: "稍后到达的回复"})
	m = next.(model)
	if m.page != approvalPage || m.viewport.YOffset != offset {
		t.Fatal("background reply changed page or history")
	}
	m = press(m, tea.KeyEnter)
	if m.page != chatPage || m.viewport.YOffset != offset || m.input.Value() != "保留这份草稿" || !strings.Contains(m.notice, "已批准") {
		t.Fatal("approval lost chat state")
	}
}

func TestComposerHasOnePromptAndCommandsNavigate(t *testing.T) {
	m := newModel()
	m = typeText(m, "第一行\n第二行\n第三行")
	if strings.Count(m.input.View(), "›") != 1 {
		t.Fatalf("repeated input prompt: %s", m.input.View())
	}
	for _, tc := range []struct {
		command string
		target  page
	}{{"/assistant", assistantsPage}, {"/approval", approvalPage}, {"/help", helpPage}} {
		m = newModel()
		m.input.SetValue(tc.command)
		m = press(m, tea.KeyEnter)
		if m.page != tc.target || m.input.Value() != "" {
			t.Fatalf("command %s failed", tc.command)
		}
		m = press(m, tea.KeyEsc)
		if m.page != chatPage || !m.input.Focused() {
			t.Fatal("command return")
		}
	}
}

func TestHistoryScrollAndQuit(t *testing.T) {
	m := newModel()
	m.resize(70, 20)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = next.(model)
	if m.viewport.AtBottom() {
		t.Fatal("history did not scroll")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlEnd})
	m = next.(model)
	if !m.viewport.AtBottom() {
		t.Fatal("did not return to latest message")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("quit command missing")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("not a quit message")
	}
}

func TestComposerGrowsShrinksAndKeepsOverflow(t *testing.T) {
	m := newModel()
	m.resize(42, 18)
	if m.input.Height() != 1 {
		t.Fatal("empty input is not one row")
	}
	m = typeText(m, "第一行")
	m = press(m, tea.KeyCtrlJ)
	m = typeText(m, "第二行")
	if m.input.Height() != 2 || !strings.Contains(m.input.View(), "第一行") || !strings.Contains(m.input.View(), "第二行") {
		t.Fatalf("two-row expansion lost visible input: %s", m.input.View())
	}
	m = press(m, tea.KeyCtrlJ)
	m = typeText(m, "第三行")
	m = press(m, tea.KeyCtrlJ)
	m = typeText(m, "第四行")
	m = press(m, tea.KeyCtrlJ)
	m = typeText(m, "第五行")
	m = press(m, tea.KeyCtrlJ)
	m = typeText(m, "第六行")
	if m.input.Height() != 5 || m.input.LineCount() != 6 || m.input.Value() != "第一行\n第二行\n第三行\n第四行\n第五行\n第六行" || !strings.Contains(m.input.View(), "第六行") {
		t.Fatalf("overflow was lost or hidden: %s", m.input.View())
	}
	if w, h := lipgloss.Size(m.View()); w > 42 || h > 18 {
		t.Fatalf("expanded input overflowed terminal: %dx%d", w, h)
	}
	for _, want := range []int{5, 4, 3, 2, 1} {
		m = press(m, tea.KeyCtrlU)
		m = press(m, tea.KeyBackspace)
		if m.input.Height() != want {
			t.Fatalf("delete: want %d rows, got %d", want, m.input.Height())
		}
	}
	if m.input.Value() != "第一行" || !strings.Contains(m.input.View(), "第一行") {
		t.Fatal("shrink lost draft or kept stale scroll position")
	}
	m = press(m, tea.KeyEnter)
	if m.input.Height() != 1 || m.input.Value() != "" || !m.assistants[0].busy {
		t.Fatal("send did not reset composer")
	}
}

func TestComposerWrapResizeAndDraftRestore(t *testing.T) {
	m := newModel()
	m.resize(200, 30)
	draft := strings.Repeat("中", 70)
	m = typeText(m, draft)
	if m.input.Height() != 1 {
		t.Fatal("wide terminal unexpectedly wrapped")
	}
	m.resize(80, 24)
	if m.input.Height() != 2 {
		t.Fatal("wrapped input did not expand to two rows")
	}
	m.resize(42, 18)
	if m.input.Height() != 4 || m.input.Value() != draft {
		t.Fatal("narrow terminal truncated draft or failed to cap height")
	}
	m = press(m, tea.KeyCtrlK)
	m = typeText(m, "开发")
	m = press(m, tea.KeyEnter)
	if m.input.Height() != 1 {
		t.Fatal("new assistant inherited composer height")
	}
	m = press(m, tea.KeyCtrlK)
	m = typeText(m, "研究")
	m = press(m, tea.KeyEnter)
	if m.input.Height() != 4 || m.input.Value() != draft {
		t.Fatal("assistant switch lost long draft")
	}
	m.resize(200, 30)
	if m.input.Height() != 1 || m.input.Value() != draft {
		t.Fatal("wide resize did not shrink without losing draft")
	}
	m = typeText(m, "末尾")
	if m.input.Value() != draft+"末尾" {
		t.Fatal("resize moved the logical caret")
	}
}

func TestMouseWheelScrollsActivePageWithoutEditingOrApproving(t *testing.T) {
	m := newModel()
	m.resize(70, 20)
	m.assistants[0].messages = append(m.assistants[0].messages, message{role: "assistant", text: strings.Repeat("历史消息\n", 60)})
	m.refresh(true)
	m.input.SetValue("保留草稿")
	before := m.viewport.YOffset
	wheel := tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress, X: 5, Y: 5}
	next, _ := m.Update(wheel)
	m = next.(model)
	if m.viewport.YOffset >= before || m.input.Value() != "保留草稿" {
		t.Fatal("wheel did not scroll history independently")
	}
	m = press(m, tea.KeyF1)
	wheel.Button = tea.MouseButtonWheelDown
	next, _ = m.Update(wheel)
	m = next.(model)
	if m.help.YOffset == 0 {
		t.Fatal("help did not scroll")
	}
	m = press(m, tea.KeyEsc)
	m = press(m, tea.KeyCtrlK)
	next, _ = m.Update(wheel)
	m = next.(model)
	if m.pickerCursor != 1 || m.selected != 0 {
		t.Fatal("wheel should move selection without opening assistant")
	}
	m = press(m, tea.KeyEsc)
	m = press(m, tea.KeyCtrlP)
	next, _ = m.Update(wheel)
	m = next.(model)
	if m.approvalChoice != 0 || m.assistants[0].approval != "pending" {
		t.Fatal("wheel changed approval")
	}
}

func TestDragSelectionCopyAndAnyClickCancel(t *testing.T) {
	m := newModel()
	m.assistants[0].messages = []message{{role: "assistant", text: "甲乙 é 👩‍💻 文本\n第二行"}}
	m.refresh(true)
	m.input.SetValue("保留草稿")
	copied := ""
	m.writeClipboard = func(value string) error { copied = value; return nil }
	mouse := func(button tea.MouseButton, action tea.MouseAction, x, y int) {
		next, _ := m.Update(tea.MouseMsg{Button: button, Action: action, X: x, Y: y})
		m = next.(model)
	}
	drag := func() {
		mouse(tea.MouseButtonLeft, tea.MouseActionPress, 3, 3)
		mouse(tea.MouseButtonLeft, tea.MouseActionMotion, 5, 3)
		mouse(tea.MouseButtonLeft, tea.MouseActionRelease, 5, 3)
	}
	drag()
	if m.selection == nil || m.selection.text() != "甲乙" {
		t.Fatal("Chinese selection split a display cell")
	}
	frame := m.View()
	m.assistants[0].busy, m.assistants[0].generation = true, 1
	next, _ := m.Update(replyMsg{assistant: 0, generation: 1, text: "迟到回复"})
	m = next.(model)
	if m.View() != frame || m.input.Value() != "保留草稿" {
		t.Fatal("incoming reply changed selected text")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(model)
	if cmd == nil {
		t.Fatal("copy command missing")
	}
	result := cmd()
	if _, ok := result.(clipboardResult); !ok || copied != "甲乙" {
		t.Fatalf("copy failed: %q", copied)
	}
	next, _ = m.Update(result)
	m = next.(model)
	if !strings.Contains(m.View(), "复制请求已发送") {
		t.Fatal("copy feedback missing")
	}
	for _, point := range [][2]int{{0, 0}, {10, 3}, {4, m.height - 2}, {4, m.height - 5}} {
		drag()
		mouse(tea.MouseButtonLeft, tea.MouseActionPress, point[0], point[1])
		mouse(tea.MouseButtonLeft, tea.MouseActionRelease, point[0], point[1])
		if m.selection != nil || !m.input.Focused() {
			t.Fatalf("click at %v did not clear selection", point)
		}
	}
	drag()
	mouse(tea.MouseButtonRight, tea.MouseActionPress, 5, 3)
	if m.selection != nil {
		t.Fatal("right click did not cancel selection")
	}
	drag()
	m = press(m, tea.KeyEsc)
	if m.selection != nil || m.page != chatPage {
		t.Fatal("Escape should only clear selection")
	}
	drag()
	next, _ = m.Update(tea.WindowSizeMsg{Width: 70, Height: 20})
	m = next.(model)
	if m.selection != nil || m.width != 70 {
		t.Fatal("resize retained stale selection coordinates")
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(model)
	if cmd != nil || m.input.Value() != "" {
		t.Fatal("Ctrl+C should clear the draft before exiting")
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("empty Ctrl+C should exit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("empty Ctrl+C should exit")
	}
}

func TestSelectionKeepsGraphemesAndReversedMultilineRanges(t *testing.T) {
	s := textSelection{lines: []string{"甲é👩‍💻末尾", "下一行"}, anchor: textPoint{x: 4, y: 0}, end: textPoint{x: 2, y: 0}, right: 99}
	if s.text() != "é👩‍💻" {
		t.Fatalf("grapheme selection: %q", s.text())
	}
	s.anchor = textPoint{x: 3, y: 1}
	s.end = textPoint{x: 2, y: 0}
	if s.text() != "é👩‍💻末尾\n下一" {
		t.Fatalf("reversed multiline selection: %q", s.text())
	}
}

func TestTerminalModifiedEnterSequences(t *testing.T) {
	for _, tc := range []struct{ name, sequence string }{{"alt carriage return", "\x1b\r"}, {"alt line feed", "\x1b\n"}, {"control J", "\n"}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			input := "first" + tc.sequence + "second\x11"
			program := tea.NewProgram(newModel(), tea.WithContext(ctx), tea.WithInput(strings.NewReader(input)), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
			result, err := program.Run()
			if err != nil {
				t.Fatal(err)
			}
			m := result.(model)
			if m.composerText() != "first\nsecond" || m.assistants[0].busy {
				t.Fatalf("modified Enter must insert a newline without sending: %q", m.composerText())
			}
		})
	}
}

func TestModifiedEnterAfterPasteBlockAndAtLimit(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter, Alt: true}, {Type: tea.KeyCtrlJ, Alt: true}, {Type: tea.KeyCtrlJ}} {
		m := newModel()
		m = pasteText(m, strings.Repeat("x", 101))
		next, _ := m.Update(key)
		m = next.(model)
		m = typeText(m, "tail")
		if m.composerText() != strings.Repeat("x", 101)+"\ntail" || len(m.assistants[0].pastes) != 1 {
			t.Fatalf("%s split a paste block or lost newline", key.String())
		}
		m.clearComposerDraft()
		m = pasteText(m, strings.Repeat("x", 4000))
		next, _ = m.Update(key)
		m = next.(model)
		if len([]rune(m.composerText())) != 4000 || !strings.Contains(m.notice, "4000") {
			t.Fatalf("%s bypassed text limit", key.String())
		}
	}
}
