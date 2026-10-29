package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	assistantapi "molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/runtimeclient"
)

func TestStartupRefreshLoadsRestoredAssistant(t *testing.T) {
	for _, dirty := range []bool{false, true} {
		t.Run(fmt.Sprintf("event_during_initial_load=%t", dirty), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/assistants":
					fmt.Fprint(w, `{"Items":[{"ID":"a","ConversationID":"first","Name":"First"},{"ID":"b","ConversationID":"restored","Name":"Restored"}]}`)
				case "/api/conversations", "/api/notifications":
					fmt.Fprint(w, `{"Items":[]}`)
				case "/api/assistants/a/status", "/api/assistants/b/status":
					fmt.Fprint(w, `{"State":"idle"}`)
				case "/api/assistants/a/chat-channel", "/api/assistants/b/chat-channel":
					fmt.Fprint(w, `{"Channel":"webui"}`)
				case "/api/permission-requests":
					fmt.Fprint(w, `[]`)
				case "/api/conversations/first/messages", "/api/conversations/restored/messages":
					fmt.Fprint(w, `{"Items":[{"ID":"message","Sequence":1,"Sender":"assistant","Content":{"Kind":"text","Text":"Loaded history"}}]}`)
				default:
					t.Errorf("unexpected route %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			client, err := runtimeclient.New(server.URL, "")
			if err != nil {
				t.Fatal(err)
			}
			m := newConnectedModel(t.Context(), client, make(chan runtimeclient.Event))
			m.preferredAssistant = "b"
			m.live.hidden = true // Do not mark messages read during this loading check.
			initial := m.loadLive(false)
			if dirty {
				next, _, _ := m.updateLive(liveEvent{Connected: true})
				m = next.(model)
			}
			next, refresh, _ := m.updateLive(initial())
			m = next.(model)
			if m.assistants[m.selected].id != "b" || refresh == nil {
				t.Fatal("restored assistant has no refresh command")
			}
			next, load, _ := m.updateLive(refresh())
			m = next.(model)
			if load == nil {
				t.Fatal("refresh did not load the restored assistant")
			}
			next, _, _ = m.updateLive(load())
			m = next.(model)
			a := m.assistants[m.selected]
			if !a.loaded || a.channel != "webui" || len(a.messages) != 1 || m.channelNotice() != "" {
				t.Fatal("restored chat did not finish loading")
			}
			if m.live.loading || m.live.scheduled || m.live.dirty || m.scheduleLive() == nil {
				t.Fatal("subsequent refresh is blocked")
			}
		})
	}
}

func TestConnectedHistorySendRetryAndSwitch(t *testing.T) {
	keys := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/notifications":
			fmt.Fprint(w, `{"Items":[]}`)
			return
		case "/api/assistants":
			fmt.Fprint(w, `{"Items":[{"ID":"a","ConversationID":"chat","Name":"真实助理","State":"active"},{"ID":"b","ConversationID":"other","Name":"另一个助理","State":"active"}]}`)
		case "/api/conversations":
			fmt.Fprint(w, `{"Items":[{"ID":"chat","LastSequence":2,"LastReadSequence":2}]}`)
		case "/api/assistants/a/chat-channel":
			fmt.Fprint(w, `{"Channel":"webui"}`)
		case "/api/assistants/a/status":
			fmt.Fprint(w, `{"State":"idle"}`)
		case "/api/permission-requests":
			fmt.Fprint(w, `[]`)
		case "/api/conversations/chat/messages":
			fmt.Fprint(w, `{"Items":[{"ID":"m2","Sequence":2,"Sender":"assistant","Content":{"Kind":"text","Text":"真实回复"}},{"ID":"m1","Sequence":1,"Sender":"user","Content":{"Kind":"text","Text":"历史"}}],"NextCursor":"older"}`)
		case "/api/conversations/messages":
			var body conversation.SendUserRequest
			json.NewDecoder(r.Body).Decode(&body)
			keys = append(keys, body.Mutation.IdempotencyKey)
			if len(keys) == 1 {
				w.WriteHeader(503)
				fmt.Fprint(w, `{"code":"unavailable"}`)
				return
			}
			json.NewEncoder(w).Encode(conversation.Message{ID: "m3", ConversationID: body.ConversationID, Sequence: 3, Sender: conversation.User, Content: body.Content})
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client, _ := runtimeclient.New(server.URL, "")
	m := newConnectedModel(t.Context(), client, make(chan runtimeclient.Event))
	result := m.loadLive(false)()
	next, _, _ := m.updateLive(result)
	m = next.(model)
	if m.input.Width()+2 != m.contentWidth() {
		t.Fatal("snapshot reset composer width")
	}
	if len(m.assistants) != 2 || m.assistants[0].messages[0].text != "历史" || strings.Contains(m.View(), "演示模式") {
		t.Fatal("snapshot not applied")
	}
	m.input.SetValue("发送内容")
	next, cmd := m.send()
	m = next.(model)
	next, _, _ = m.updateLive(cmd())
	m = next.(model)
	if m.input.Value() != "发送内容" || !strings.Contains(m.notice, "发送未确认") {
		t.Fatal("failed send lost draft")
	}
	next, cmd = m.send()
	m = next.(model)
	m.selectAssistant(1)
	m.input.SetValue("另一份草稿")
	next, _, _ = m.updateLive(cmd())
	m = next.(model)
	if keys[0] != keys[1] || m.input.Value() != "另一份草稿" || m.assistants[0].composer.Value() != "" || len(m.assistants[0].messages) != 3 {
		t.Fatal("retry or navigation corrupted messages/drafts")
	}
	// Empty account and background snapshots remain navigable.
	next, _, _ = m.updateLive(snapshotMsg{assistants: []assistantapi.Assistant{}})
	m = next.(model)
	if !strings.Contains(m.View(), "尚无助理") {
		t.Fatal("empty account missing")
	}
	m = press(m, tea.KeyCtrlP)
	_ = m.View()
}

func TestLiveSelectionFreezeAndApprovalExactScope(t *testing.T) {
	var sent policy.PermissionResolution
	var requests []policy.PermissionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(requests)
			return
		}
		sent = policy.PermissionResolution{}
		json.NewDecoder(r.Body).Decode(&sent)
		json.NewEncoder(w).Encode(policy.PermissionRequest{ID: sent.RequestID, State: "approved"})
	}))
	defer server.Close()
	c, _ := runtimeclient.New(server.URL, "")
	m := newConnectedModel(context.Background(), c, make(chan runtimeclient.Event))
	expiry := time.Now().Add(time.Hour)
	q := policy.PermissionRequest{ID: "p", AssistantID: "a", State: "pending", Version: 7, Input: policy.PermissionRequestInput{ResourceKind: "storage", ResourceID: "disk", ResourceDescription: "测试目录", Actions: []policy.Action{policy.Read}, PathPrefixes: []string{"/reports"}, ExpiresAt: &expiry, Additional: []policy.PermissionRequestInput{{ResourceKind: "compute", ResourceID: "cpu", FullAccess: true, Actions: []policy.Action{policy.Execute}, ExpiresAt: &expiry}}}}
	m.assistants = []assistant{{id: "a", chat: "chat", name: "助理", loaded: true, composerState: composerState{composer: newComposer()}, requests: []policy.PermissionRequest{q}}}
	requests = []policy.PermissionRequest{q}
	next, load := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = next.(model)
	applyManaged(t, &m, load)
	m.manage.screen.rows[0].open(&m)
	m.manage.screen.rows[0].open(&m)
	// A later request must not replace the one the user is reviewing.
	m.assistants[0].requests = []policy.PermissionRequest{{ID: "new", Version: 9}}
	cmd, _ := m.updateManaged(tea.KeyMsg{Type: tea.KeyEnter})
	result := cmd().(managedResult)
	if result.err != nil || sent.RequestID != "p" || sent.Mutation.ExpectedVersion != 7 || sent.Scope.PathPrefixes[0] != "/reports" || !sent.Scope.ReadOnly || !sent.Additional[0].Scope.FullAccess || !sent.ExpiresAt.Equal(expiry) || !sent.Additional[0].ExpiresAt.Equal(expiry) {
		t.Fatalf("wrong resolution %+v", sent)
	}
	m.liveError(&runtimeclient.Error{Status: 401, Code: "unauthenticated"})
	m.input.SetValue("不可发送")
	_, cmd = m.sendLive("不可发送")
	if !m.live.expired || cmd != nil {
		t.Fatal("expired session still sends")
	}
	m.live.expired = false
	m.requestScreen(q)
	m.manage.screen.rows[len(m.manage.screen.rows)-1].open(&m)
	cmd, _ = m.updateManaged(tea.KeyMsg{Type: tea.KeyEnter})
	cmd()
	if sent.Decision != "reject" || sent.ResourceID != "" || len(sent.Additional) != 0 {
		t.Fatal("rejection carried grants")
	}
}

func TestRunConnectedRejectsNonTerminalAndLiveErrors(t *testing.T) {
	c, _ := runtimeclient.New("http://127.0.0.1:1", "")
	if err := RunConnected(t.Context(), strings.NewReader(""), &strings.Builder{}, c); err == nil {
		t.Fatal("non-terminal accepted")
	}
	m := newConnectedModel(t.Context(), c, make(chan runtimeclient.Event))
	next, _, _ := m.updateLive(snapshotMsg{err: errors.New("离线")})
	m = next.(model)
	if m.live.loading || !strings.Contains(m.notice, "离线") || !strings.Contains(m.View(), "离线") {
		t.Fatal("failed snapshot stuck")
	}
	old := []message{{id: core.ID("m"), sequence: 1, text: "old"}}
	merged := mergeMessages(old, []conversation.Message{{ID: "m", Sequence: 1, Content: conversation.Content{Text: "new"}}})
	if len(merged) != 1 || merged[0].text != "new" {
		t.Fatal("duplicate event duplicated message")
	}
}

func TestHistoryPaginationPreservesReaderAndBackgroundDraft(t *testing.T) {
	m := newConnectedModel(t.Context(), nil, make(chan runtimeclient.Event))
	m.assistants = []assistant{{id: "a", chat: "chat", name: "A", loaded: true, composerState: composerState{composer: newComposer()}, sequence: 100, older: "older"}, {id: "b", chat: "other", name: "B", loaded: true, composerState: composerState{composer: newComposer()}}}
	for i := 51; i <= 100; i++ {
		m.assistants[0].messages = append(m.assistants[0].messages, message{id: core.ID(fmt.Sprint(i)), sequence: uint64(i), role: "assistant", text: fmt.Sprintf("message %03d", i)})
	}
	m.refresh(true)
	m.viewport.GotoTop()
	before := m.viewport.View()
	older := snapshotMsg{target: "a", history: true}
	for i := 1; i <= 50; i++ {
		older.messages = append(older.messages, conversation.Message{ID: core.ID(fmt.Sprint(i)), Sequence: uint64(i), Sender: conversation.Assistant, Content: conversation.Content{Text: fmt.Sprintf("message %03d", i)}})
	}
	next, _, _ := m.updateLive(older)
	m = next.(model)
	if m.viewport.View() != before || len(m.assistants[0].messages) != 100 || m.assistants[0].older != "" {
		t.Fatal("older page moved the visible reading position")
	}
	m.input.SetValue("A draft")
	m.selectAssistant(1)
	m.input.SetValue("B draft")
	update := snapshotMsg{target: "a", assistants: []assistantapi.Assistant{{ID: "a", ConversationID: "chat", Name: "A"}, {ID: "b", ConversationID: "other", Name: "B"}}, messages: []conversation.Message{{ID: "101", Sequence: 101, Sender: conversation.Assistant, Content: conversation.Content{Text: "late reply"}}}}
	next, _, _ = m.updateLive(update)
	m = next.(model)
	if m.assistants[m.selected].id != "b" || m.input.Value() != "B draft" || strings.Contains(m.View(), "late reply") || m.assistants[0].composer.Value() != "A draft" {
		t.Fatal("background response changed active chat")
	}
}
