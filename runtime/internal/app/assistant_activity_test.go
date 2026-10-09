package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
)

func TestAssistantActivityPageIsolationAndPreview(t *testing.T) {
	f := newMultiUserFixture(t)
	ctx := t.Context()
	first := f.assistant(t, f.alice, "first")
	empty := f.assistant(t, f.alice, "empty")
	archived := f.assistant(t, f.alice, "archived")
	foreign := f.assistant(t, f.bob, "foreign")
	_, err := f.b.Services.Assistants.Dismiss(ctx, f.alice, assistant.LifecycleRequest{ID: archived.ID, Mutation: core.Mutation{IdempotencyKey: "dismiss", ExpectedVersion: archived.Version}})
	need(t, err)
	for index, text := range []string{"old", "  " + strings.Repeat("猫", 85) + "\nprivate tail"} {
		_, err := f.b.Services.Conversations.SendUser(ctx, f.alice, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "message-" + strconv.Itoa(index)}, ConversationID: first.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: text}})
		need(t, err)
	}
	type row struct {
		AssistantID, ConversationID core.ID
		Dreaming                    bool
		Preview                     *struct{ Kind, Text string }
	}
	get := func(path string, authenticated bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "https://runtime.example"+path, nil)
		r.RemoteAddr = "127.0.0.1:10000"
		r.Header.Set("X-Forwarded-Proto", "https")
		if authenticated {
			r.AddCookie(&http.Cookie{Name: "__Host-molr_session", Value: f.aliceSession.Token.Reveal()})
		}
		w := httptest.NewRecorder()
		f.b.HTTP.Handler().ServeHTTP(w, r)
		return w
	}
	if response := get("/api/assistants/activity", false); response.Code != http.StatusUnauthorized {
		t.Fatal(response.Code)
	}
	seen := map[core.ID]row{}
	cursor := ""
	for i := 0; i < 3; i++ {
		response := get("/api/assistants/activity?limit=1&cursor="+url.QueryEscape(cursor), true)
		if response.Code != 200 {
			t.Fatalf("%d %s", response.Code, response.Body.String())
		}
		var page core.Page[row]
		need(t, json.Unmarshal(response.Body.Bytes(), &page))
		if len(page.Items) != 1 {
			t.Fatal(page)
		}
		seen[page.Items[0].AssistantID] = page.Items[0]
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 2 || seen[first.ID].Preview == nil || seen[first.ID].Preview.Text != strings.Repeat("猫", 80)+"…" || seen[empty.ID].Preview != nil {
		t.Fatalf("unexpected summaries: %+v", seen)
	}
	if _, ok := seen[foreign.ID]; ok {
		t.Fatal("foreign assistant leaked")
	}
	if _, ok := seen[archived.ID]; ok {
		t.Fatal("dismissed assistant leaked")
	}
	if _, err := f.b.Services.Agent.Activities(ctx, f.alice, []core.ID{foreign.ID}); err == nil {
		t.Fatal("foreign activity read accepted")
	}
	if response := get("/api/assistants/activity?limit=201", true); response.Code != 400 {
		t.Fatal(response.Code)
	}
}
