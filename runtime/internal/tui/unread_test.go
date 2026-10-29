package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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

func TestReturningToCurrentChatUpdatesDemoUnread(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyEsc, tea.KeyEnter} {
		for _, history := range []bool{false, true} {
			m := newModel()
			m.assistants[0].messages = []message{{role: "assistant", text: strings.Repeat("history\n\n", 60)}}
			m.refresh(true)
			if history {
				m.viewport.HalfPageUp()
			}
			offset := m.viewport.YOffset
			m = press(m, tea.KeyCtrlK)
			m.assistants[0].busy = true
			m.assistants[0].generation++
			next, _ := m.Update(replyMsg{assistant: 0, generation: m.assistants[0].generation, text: "new reply"})
			m = next.(model)
			if m.assistants[0].unreadCount != 1 {
				t.Fatal("picker consumed unread reply")
			}
			m = press(m, key)
			if history {
				if m.viewport.YOffset != offset || m.assistants[0].unreadCount != 1 {
					t.Fatal("returning to history changed position or marked unread reply read")
				}
			} else if !m.viewport.AtBottom() || m.assistants[0].unreadCount != 0 {
				t.Fatalf("key=%v: returning to current chat did not follow and clear unread", key)
			}
		}
	}
}

func TestReturningToCurrentChatReportsReadAndUpdatesCount(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyEsc, tea.KeyEnter} {
		var read conversation.ReadRequest
		m := workflowModel(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/conversations/read" {
				t.Errorf("unexpected request %s", r.URL.Path)
			}
			if err := json.NewDecoder(r.Body).Decode(&read); err != nil {
				t.Error(err)
			}
			fmt.Fprint(w, `{}`)
		})
		m.assistants[0].sequence = 9
		m.assistants[0].unreadSequence = 9
		m.assistants[0].unreadCount = 2
		m.refresh(true)
		m = press(m, tea.KeyCtrlK)
		next, cmd := m.Update(tea.KeyMsg{Type: key})
		m = next.(model)
		if m.assistants[0].unreadCount != 2 {
			t.Fatal("count cleared before read succeeded")
		}
		results := unreadCommandResults(cmd)
		if len(results) != 1 || read.ConversationID != "chat" || read.ThroughSequence != 9 {
			t.Fatalf("key=%v: missing read on returning to current chat: %+v", key, results)
		}
		next, _, _ = m.updateLive(results[0])
		m = next.(model)
		if m.assistants[0].unreadCount != 0 || m.assistants[0].readSequence != 9 {
			t.Fatal("successful read did not update local unread count")
		}
		m = press(m, tea.KeyCtrlK)
		if strings.Contains(ansi.Strip(m.pickerView()), "条未读") {
			t.Fatal("returning to picker still shows read messages as unread")
		}
	}
}

func unreadCommandResults(cmd tea.Cmd) []readMsg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case readMsg:
		return []readMsg{msg}
	case tea.BatchMsg:
		var results []readMsg
		for _, child := range msg {
			results = append(results, unreadCommandResults(child)...)
		}
		return results
	}
	return nil
}

func TestReadAcknowledgementPreservesNewerUnreadAndRejectsStaleSnapshots(t *testing.T) {
	for _, newer := range []bool{false, true} {
		m := newConnectedModel(t.Context(), nil, make(chan runtimeclient.Event))
		m.page = assistantsPage
		m.assistants = []assistant{{id: "a", chat: "chat", sequence: 9, unreadSequence: 9, unreadCount: 2, reading: true, composer: newComposer()}}
		last := uint64(9)
		if newer {
			last = 10
			m.assistants[0].unreadSequence = last
			m.assistants[0].unreadCount = 3
		}
		next, cmd, _ := m.updateLive(readMsg{id: "a", sequence: 9})
		m = next.(model)
		if cmd == nil || !m.live.scheduled || m.assistants[0].reading {
			t.Fatal("read acknowledgement did not refresh authoritative counts")
		}
		want := uint64(0)
		if newer {
			want = 3 // Wait for the server's exact count beyond the acknowledged boundary.
		}
		if m.assistants[0].unreadCount != want {
			t.Fatal("old acknowledgement cleared newer unread messages")
		}
		snapshot := snapshotMsg{
			assistants: []assistantapi.Assistant{{ID: "a", ConversationID: "chat", Name: "A"}},
			chats:      []conversation.Conversation{{ID: "chat", LastSequence: last, UnreadCount: 3}},
			target:     "a", conversation: conversation.Conversation{ID: "chat", LastSequence: last, UnreadCount: 3},
		}
		next, _, _ = m.updateLive(snapshot)
		m = next.(model)
		if m.assistants[0].unreadCount != want {
			t.Fatal("stale snapshot restored an acknowledged unread count")
		}
		want = 0
		if newer {
			want = 1
		}
		snapshot.conversation.LastReadSequence = 9
		snapshot.conversation.UnreadCount = want
		snapshot.chats[0] = snapshot.conversation
		next, _, _ = m.updateLive(snapshot)
		if next.(model).assistants[0].unreadCount != want {
			t.Fatal("fresh snapshot did not apply exact unread count")
		}
	}
}

func TestFailedReadPreservesUnreadCount(t *testing.T) {
	m := newConnectedModel(t.Context(), nil, make(chan runtimeclient.Event))
	m.assistants = []assistant{{id: "a", chat: "chat", sequence: 9, unreadSequence: 9, unreadCount: 2, composer: newComposer()}}
	m.refresh(true)
	m.returnToLatest()
	if m.assistants[0].unreadCount != 2 {
		t.Fatal("return to latest cleared unread before confirmation")
	}
	next, cmd, _ := m.updateLive(readMsg{id: "a", sequence: 9, err: errors.New("read failed")})
	m = next.(model)
	if m.assistants[0].unreadCount != 2 || m.assistants[0].readSequence != 0 || m.assistants[0].reading || cmd != nil {
		t.Fatal("failed read changed count or retried without a new action")
	}
}

func TestSwitchingAwayFromPickerPreservesOriginalFollowState(t *testing.T) {
	m := newModel()
	m.assistants[0].messages = []message{{role: "assistant", text: strings.Repeat("history\n\n", 60)}}
	m.refresh(true)
	m = press(m, tea.KeyCtrlK)
	m.assistants[0].busy = true
	m.assistants[0].generation++
	next, _ := m.Update(replyMsg{assistant: 0, generation: m.assistants[0].generation, text: "new reply"})
	m = next.(model)
	m.pickerCursor = 1
	m = press(m, tea.KeyEnter)
	m = press(m, tea.KeyCtrlK)
	m.pickerCursor = 0
	m = press(m, tea.KeyEnter)
	if !m.viewport.AtBottom() || m.assistants[0].unreadCount != 0 {
		t.Fatal("message arriving in picker lost the original follow state")
	}
}
