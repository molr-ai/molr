package tui

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/notification"
)

func TestChatStatusClickOpensDetailsWithoutMutation(t *testing.T) {
	for _, tc := range []struct{ action, title string }{
		{"/approval", "待审批申请"}, {"/status", "执行状态"}, {"/channel", "聊天入口"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			requests := 0
			m := workflowModel(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != "GET" {
					t.Errorf("click mutated service: %s %s", r.Method, r.URL.Path)
				}
				switch r.URL.Path {
				case "/api/permission-requests":
					fmt.Fprint(w, `[]`)
				case "/api/notifications":
					fmt.Fprint(w, `{"Items":[]}`)
				case "/api/assistants/a/chat-channel":
					fmt.Fprint(w, `{"Channel":"weixin"}`)
				case "/api/assistants/a/status":
					fmt.Fprint(w, `{"State":"idle"}`)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
				}
			})
			m.resize(42, 18)
			a := &m.assistants[0]
			a.messages = []message{{role: "assistant", text: strings.Repeat("history\n\n", 40)}}
			switch tc.action {
			case "/approval":
				a.approval = "pending"
			case "/status":
				a.notices = []notification.Notice{{Kind: "task.failed"}}
			case "/channel":
				a.channel = "weixin"
			}
			m.refresh(true)
			m.input.SetValue("保留草稿")
			m.viewport.HalfPageUp()
			offset := m.viewport.YOffset
			status, action := m.chatStatus()
			if action != tc.action {
				t.Fatalf("action=%q status=%q", action, status)
			}
			y := 3 + m.viewport.Height + 1
			// The truncated text and the return button are separated by inactive space.
			next, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 2 + cellWidth(status), Y: y})
			m = next.(model)
			if m.page != chatPage || m.viewport.YOffset != offset {
				t.Fatal("gap triggered navigation")
			}
			next, cmd := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 2, Y: y})
			m = next.(model)
			if m.page != managePage || m.manage.screen.title != tc.title || cmd == nil {
				t.Fatal("wrong detail destination")
			}
			next, _ = m.Update(cmd())
			m = next.(model)
			if requests == 0 {
				t.Fatal("details not fetched")
			}
			m = press(m, tea.KeyEsc)
			if m.page != chatPage || m.input.Value() != "保留草稿" || m.viewport.YOffset != offset {
				t.Fatal("return lost draft or reading position")
			}
		})
	}
}

func TestChatStatusOverridesDoNotClickHiddenApproval(t *testing.T) {
	for _, change := range []func(*model){
		func(m *model) { m.notice = "请求失败" },
		func(m *model) { m.draftError = "保存失败" },
		func(m *model) { m.assistants[0].busy = true },
		func(m *model) { m.input.SetValue("/hel") },
		func(m *model) { m.assistants[0].status.Compression = &agent.CompressionResult{State: "running"} },
		func(m *model) { m.assistants[0].status.Expedite = &agent.ExpediteStatus{State: "pending"} },
	} {
		m := newModel()
		m.resize(42, 18)
		m.assistants[0].approval = "pending"
		change(&m)
		_, action := m.chatStatus()
		if action != "" {
			t.Fatalf("hidden approval remains clickable: %q", action)
		}
		next, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 2, Y: 3 + m.viewport.Height + 1})
		if next.(model).page != chatPage {
			t.Fatal("passive status navigated")
		}
	}
}

func TestDemoApprovalClickAndSelectionProtection(t *testing.T) {
	m := newModel()
	m.input.SetValue("draft")
	m.refresh(true)
	y := 3 + m.viewport.Height + 1
	next, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 2, Y: 3})
	m = next.(model)
	next, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 2, Y: y})
	m = next.(model)
	if m.page != chatPage || m.selection != nil {
		t.Fatal("first click should clear selection only")
	}
	next, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 2, Y: y})
	m = next.(model)
	if m.page != approvalPage || m.assistants[0].approval != "pending" {
		t.Fatal("click did not open approval or changed its decision")
	}
	m = press(m, tea.KeyEsc)
	if m.input.Value() != "draft" {
		t.Fatal("click cleared draft")
	}
}
