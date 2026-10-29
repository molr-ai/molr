package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	assistantapi "molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/runtimeclient"
)

func TestPickerUnreadBadgeTakesPriorityAtRowEnd(t *testing.T) {
	for _, width := range []int{42, 70, 120} {
		for _, count := range []uint64{0, 1, 1234} {
			for _, busy := range []bool{false, true} {
				for _, name := range []string{"研究助理", strings.Repeat("长名称", 30)} {
					m := newModel()
					m.resize(width, 24)
					m.openPage(assistantsPage)
					m.assistants[0].name = name
					m.assistants[0].unreadCount = count
					m.assistants[0].busy = busy
					view := ansi.Strip(m.pickerView())
					line := strings.Split(view, "\n")[4]
					status := "待审批"
					if busy {
						status = "正在处理"
					}
					if count > 0 {
						status = fmt.Sprintf("%d 条未读 ", count)
					}
					if !strings.HasSuffix(line, status) || cellWidth(line) > m.contentWidth() {
						t.Fatalf("width=%d count=%d: %q", width, count, line)
					}
					if count == 0 {
						if strings.Contains(view, "条未读") {
							t.Fatal("zero unread badge visible")
						}
						continue
					}
					badge := fmt.Sprintf("%d 条未读", count)
					if !strings.Contains(line, badge) || strings.Contains(line, "新消息") {
						t.Fatalf("missing exact unread count: %q", line)
					}
					if cellWidth(line) != m.contentWidth() || strings.Contains(line, "正在处理") || strings.Contains(line, "待审批") || strings.Contains(line, "当前") {
						t.Fatalf("unread badge did not take priority at row end: %q", line)
					}
				}
			}
		}
	}
}

func TestLivePickerUnreadCountsRefreshWithoutAccumulating(t *testing.T) {
	m := newConnectedModel(t.Context(), nil, make(chan runtimeclient.Event))
	m.page = assistantsPage
	for _, counts := range [][2]uint64{{2, 123}, {2, 123}, {3, 124}, {0, 0}} {
		snapshot := snapshotMsg{
			assistants: []assistantapi.Assistant{{ID: "a", ConversationID: "chat-a", Name: "A"}, {ID: "b", ConversationID: "chat-b", Name: "B"}},
			chats:      []conversation.Conversation{{ID: "chat-a", UnreadCount: counts[0]}, {ID: "chat-b", UnreadCount: counts[1]}},
			target:     "a", conversation: conversation.Conversation{ID: "chat-a", UnreadCount: counts[0]},
		}
		next, _, _ := m.updateLive(snapshot)
		m = next.(model)
		for i, want := range counts {
			if got := m.assistants[i].unreadCount; got != want {
				t.Fatalf("assistant %d: got %d want %d", i, got, want)
			}
		}
	}
}

func TestDemoUnreadCountsAccumulateAndClearOnReading(t *testing.T) {
	m := newModel()
	m.openPage(assistantsPage)
	for count := uint64(1); count <= 3; count++ {
		m.assistants[1].busy = true
		m.assistants[1].generation++
		next, _ := m.Update(replyMsg{assistant: 1, generation: m.assistants[1].generation, text: "reply"})
		m = next.(model)
		if m.assistants[1].unreadCount != count {
			t.Fatal("unread replies did not accumulate")
		}
	}
	m.pickerCursor = 1
	m = press(m, tea.KeyEnter)
	if m.selected != 1 || m.page != chatPage || m.assistants[1].unreadCount != 0 {
		t.Fatal("opening unread conversation did not clear count")
	}
}
