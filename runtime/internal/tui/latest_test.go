package tui

import (
	"context"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/im/conversation"
	"net/http"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestLatestControlLayoutAndActions(t *testing.T) {
	for _, size := range [][2]int{{42, 18}, {71, 25}, {100, 30}} {
		for _, click := range []bool{false, true} {
			m := newModel()
			m.resize(size[0], size[1])
			m.assistants[0].messages = []message{{role: "assistant", text: strings.Repeat("历史记录\n\n", 60)}}
			m.input.SetValue("保留草稿\n第二行")
			m.refresh(true)
			height := m.viewport.Height
			if strings.Contains(ansi.Strip(m.View()), "返回底部") {
				t.Fatal("control visible at bottom")
			}
			m = press(m, tea.KeyPgUp)
			m.notice = "处理中"
			label, x, y := m.latestControl()
			lines := strings.Split(ansi.Strip(m.View()), "\n")
			if label == "" || cellCut(lines[y], x, x+cellWidth(label)) != label || m.width-2-cellWidth(label) != x {
				t.Fatalf("control not right-aligned with two-column inset: %q at %d,%d", label, x, y)
			}
			if !strings.Contains(lines[y], m.notice) {
				t.Fatal("status overwritten")
			}
			if strings.TrimSpace(lines[y+1]) != "" {
				t.Fatal("missing blank row between control and composer")
			}
			if m.viewport.Height != height {
				t.Fatal("control changed viewport height")
			}
			offset := m.viewport.YOffset
			m.assistants[0].busy = true
			next, _ := m.Update(replyMsg{assistant: 0, generation: m.assistants[0].generation, text: "新增消息"})
			m = next.(model)
			if m.viewport.YOffset != offset || !strings.Contains(ansi.Strip(m.View()), "有新消息") {
				t.Fatal("new message did not preserve reading position and show control")
			}
			if click {
				label, x, y = m.latestControl()
				next, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x - 1, Y: y})
				m = next.(model)
				if m.viewport.AtBottom() {
					t.Fatal("clicked outside control")
				}
				next, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x + cellWidth(label)/2, Y: y})
			} else {
				next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
			}
			m = next.(model)
			if !m.viewport.AtBottom() || m.assistants[0].unreadCount > 0 || strings.Contains(ansi.Strip(m.View()), "有新消息") {
				t.Fatal("did not return to bottom and clear indicator")
			}
			if m.input.Value() != "保留草稿\n第二行" || !m.input.Focused() {
				t.Fatal("lost draft or input focus")
			}
			m.assistants[0].busy = true
			next, _ = m.Update(replyMsg{assistant: 0, generation: m.assistants[0].generation, text: "继续跟随"})
			if !next.(model).viewport.AtBottom() {
				t.Fatal("did not resume following")
			}
		}
	}
}

func TestHistoryShortcutsThroughTerminalDecoder(t *testing.T) {
	for _, tc := range []struct {
		keys   string
		bottom bool
	}{
		{"\x1b[1;3A", false},
		{"\x1b[1;3A\x1b[1;3B", true},
		{"\x1b[1;3A\x07", true},
		{"\x1b[5~\x1b[1;5F", true},
	} {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		m := newModel()
		m.resize(70, 20)
		program := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(strings.NewReader("draft"+tc.keys+"\x11")), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
		result, err := program.Run()
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		m = result.(model)
		if m.viewport.AtBottom() != tc.bottom || m.input.Value() != "draft" {
			t.Fatalf("%q: bottom=%v draft=%q", tc.keys, m.viewport.AtBottom(), m.input.Value())
		}
	}
}

func TestAltPagingInHelp(t *testing.T) {
	m := newModel()
	m.resize(70, 20)
	m.page = helpPage
	m.help.GotoTop()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	m = next.(model)
	if m.help.YOffset == 0 {
		t.Fatal("help did not scroll")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp, Alt: true})
	m = next.(model)
	if !m.help.AtTop() {
		t.Fatal("help did not return")
	}
}

func TestAltPagingInDetails(t *testing.T) {
	for _, page := range []page{attachmentsPage, managePage} {
		m := newModel()
		m.resize(70, 20)
		m.page = page
		m.files.preview = strings.Repeat("line\n", 100)
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown, Alt: true})
		m = next.(model)
		if page == attachmentsPage && m.files.previewOffset == 0 || page == managePage && m.manage.screen.scroll == 0 {
			t.Fatal("detail did not scroll")
		}
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp, Alt: true})
		m = next.(model)
		if m.files.previewOffset != 0 || m.manage.screen.scroll != 0 {
			t.Fatal("detail did not return")
		}
	}
}

func TestReturnLatestReportsRead(t *testing.T) {
	for _, click := range []bool{false, true} {
		var read conversation.ReadRequest
		m := workflowModel(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/conversations/read" {
				t.Errorf("unexpected request: %s", r.URL.Path)
			}
			if err := json.NewDecoder(r.Body).Decode(&read); err != nil {
				t.Error(err)
			}
			w.Write([]byte(`{}`))
		})
		m.assistants[0].messages = []message{{role: "assistant", text: strings.Repeat("history\n\n", 60)}}
		m.assistants[0].sequence = 9
		m.assistants[0].unreadCount = 1
		m.refresh(true)
		m.viewport.HalfPageUp()
		var event tea.Msg = tea.KeyMsg{Type: tea.KeyCtrlG}
		if click {
			_, x, y := m.latestControl()
			event = tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x, Y: y}
		}
		next, cmd := m.Update(event)
		m = next.(model)
		if cmd == nil || !m.viewport.AtBottom() {
			t.Fatal("missing return/read action")
		}
		cmd()
		if read.ConversationID != "chat" || read.ThroughSequence != 9 {
			t.Fatalf("wrong read boundary: %+v", read)
		}
	}
}
