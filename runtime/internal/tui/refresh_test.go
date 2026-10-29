package tui

import (
	"context"
	"errors"
	"fmt"
	assistantapi "molr.ai/runtime/internal/business/assistant"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/runtimeclient"
	"molr.ai/runtime/internal/storage/files"
)

func TestMessageMergeDistinguishesReceiptsAndContent(t *testing.T) {
	remote := conversation.Message{ID: "one", Sequence: 2, Sender: "assistant", Content: conversation.Content{Text: "hello"}}
	old := []message{displayMessage(remote)}
	for _, incoming := range [][]conversation.Message{nil, {remote}} {
		got, changed := mergeMessageChanges(old, incoming)
		if changed || &got[0] != &old[0] {
			t.Fatal("unchanged messages were rebuilt")
		}
	}
	now := time.Now()
	remote.ReadAt = &now
	got, changed := mergeMessageChanges(old, []conversation.Message{remote})
	if changed || !got[0].read || old[0].read {
		t.Fatal("receipt must update independently without mutating previous state")
	}
	remote.Content.Text = "edited"
	earlier := remote
	earlier.ID = "earlier"
	earlier.Sequence = 1
	got, changed = mergeMessageChanges(got, []conversation.Message{remote, earlier, remote})
	if !changed || len(got) != 2 || got[0].id != "earlier" || got[1].text != "edited" {
		t.Fatalf("incorrect deduplication/order: %+v", got)
	}
}

func TestTranscriptCacheMatchesFreshRendering(t *testing.T) {
	m := newModel()
	m.assistants[m.selected].messages = []message{{role: "assistant", text: "# Heading\n\n正文 **bold**", attachments: []core.ID{"f"}}, {role: "user", text: "question"}}
	m.refresh(true)
	check := func() {
		t.Helper()
		cached := m.viewport.View()
		m.renderCache = nil
		m.refresh(true)
		if cached != m.viewport.View() {
			t.Fatal("cache differed from fresh render")
		}
	}
	check()
	m.fileCache = map[core.ID]files.File{"f": {Name: "report.txt", Size: 20}}
	m.refresh(true)
	check()
	if !strings.Contains(m.viewport.View(), "report.txt") {
		t.Fatal("attachment metadata did not invalidate rendering")
	}
	m.resize(60, 30)
	check()
	m.assistants[m.selected].messages[0].text = "Changed"
	m.refresh(true)
	check()
	m.assistants[m.selected].messages = m.assistants[m.selected].messages[:1]
	m.refresh(true)
	if len(m.renderCache) != 1 {
		t.Fatal("removed messages retained in cache")
	}
}

func TestDetailLoadsAreConcurrentAndBounded(t *testing.T) {
	var active, peak, started atomic.Int32
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		if started.Add(1) == 3 {
			close(release)
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		switch r.URL.Path {
		case "/api/conversations":
			fmt.Fprint(w, `{"Items":[{"ID":"chat"}]}`)
		case "/api/notifications":
			w.WriteHeader(500)
		case "/api/permission-requests":
			fmt.Fprint(w, `[]`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer server.Close()
	client, _ := runtimeclient.New(server.URL, "")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	out := snapshotMsg{target: "a"}
	if err := out.loadDetails(ctx, client, "chat"); err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 3 || started.Load() != 5 || out.conversation.ID != "chat" || out.noticesOK {
		t.Fatalf("invalid snapshot/concurrency: peak=%d requests=%d snapshot=%+v", peak.Load(), started.Load(), out)
	}
}

func BenchmarkTranscriptRefresh(b *testing.B) {
	for _, cache := range []bool{false, true} {
		b.Run(fmt.Sprintf("cached=%t", cache), func(b *testing.B) {
			m := newModel()
			m.assistants[m.selected].messages = nil
			for i := 0; i < 100; i++ {
				m.assistants[m.selected].messages = append(m.assistants[m.selected].messages, message{role: "assistant", text: fmt.Sprintf("## Message %d\n\nSome **formatted** text and 中文内容.", i)})
			}
			m.refresh(true)
			b.ResetTimer()
			b.ReportAllocs()
			for b.Loop() {
				if !cache {
					m.renderCache = nil
				}
				m.refresh(true)
			}
		})
	}
}
func BenchmarkEmptyMessageMerge(b *testing.B) {
	old := make([]message, 10000)
	for b.Loop() {
		mergeMessageChanges(old, nil)
	}
}

func TestDetailLoadCancellationAndErrorPriority(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		if r.URL.Path == "/api/conversations" {
			fmt.Fprint(w, `{"Code":"forbidden"}`)
		} else {
			fmt.Fprint(w, `{"Code":"unavailable"}`)
		}
	}))
	defer server.Close()
	client, _ := runtimeclient.New(server.URL, "")
	out := snapshotMsg{target: "a"}
	err := out.loadDetails(t.Context(), client, "chat")
	var apiErr *runtimeclient.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "forbidden" {
		t.Fatalf("wrong error priority: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := out.loadDetails(ctx, client, "chat"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled load: %v", err)
	}
}

func TestStatusOnlySnapshotPreservesTranscriptAndDraft(t *testing.T) {
	m := newConnectedModel(t.Context(), nil, make(chan runtimeclient.Event))
	a := assistant{id: "a", chat: "chat", name: "A", loaded: true, channel: "webui", composer: newComposer()}
	for i := 0; i < 50; i++ {
		a.messages = append(a.messages, message{id: core.ID(fmt.Sprint(i)), role: "assistant", text: fmt.Sprintf("Line %d", i)})
	}
	m.assistants = []assistant{a}
	m.input.SetValue("unsent draft")
	m.refresh(true)
	m.viewport.SetYOffset(5)
	before := m.viewport.View()
	offset := m.viewport.YOffset
	original := &m.assistants[0].messages[0]
	snapshot := snapshotMsg{target: "a", assistants: []assistantapi.Assistant{{ID: "a", ConversationID: "chat", Name: "A"}}, chats: []conversation.Conversation{{ID: "chat"}}, conversation: conversation.Conversation{ID: "chat"}}
	snapshot.channel.Channel = "webui"
	next, _, _ := m.updateLive(snapshot)
	m = next.(model)
	if m.viewport.View() != before || m.viewport.YOffset != offset || m.input.Value() != "unsent draft" {
		t.Fatal("status refresh disturbed history or draft")
	}
	if &m.assistants[0].messages[0] != original {
		t.Fatal("empty update rebuilt messages")
	}
}
