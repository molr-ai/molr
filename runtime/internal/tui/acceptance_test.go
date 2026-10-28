package tui

import (
	"bytes"
	"context"
	"fmt"
	"molr.ai/runtime/internal/im/conversation"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	assistantapi "molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/runtimeclient"
	"molr.ai/runtime/internal/workspace"
)

// Explicit opt-in: use an isolated Runtime and a test account, never a production session.
func TestTUIExistingRuntimeAcceptance(t *testing.T) {
	server, session := os.Getenv("MOLR_TUI_TEST_SERVER"), os.Getenv("MOLR_TUI_TEST_SESSION")
	if server == "" || session == "" {
		t.Skip("set isolated Runtime URL and test session")
	}
	client, err := runtimeclient.New(server, "")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := client.Restore(session); err != nil || !ok {
		t.Fatalf("restore test session: %v", err)
	}
	m := newConnectedModel(t.Context(), client, make(chan runtimeclient.Event))
	load := func(cmd tea.Cmd) {
		t.Helper()
		if cmd == nil {
			t.Fatal("missing load")
		}
		result := cmd().(managedResult)
		if result.err != nil {
			t.Fatal(result.err)
		}
		m.updateManaged(result)
	}
	load(m.assistantForm(true))
	formValue(t, &m, "name", "终端验收 "+time.Now().Format("150405"))
	formValue(t, &m, "budget", "1")
	cmd, _ := m.updateManaged(tea.KeyMsg{Type: tea.KeyEnter})
	result := cmd().(managedResult)
	if result.err != nil {
		t.Fatal(result.err)
	}
	m.updateManaged(result)
	id := m.preferredAssistant
	if id == "" {
		t.Fatal("created assistant not selected")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Do(ctx, "POST", "/api/assistants/"+string(id)+"/stop", struct{}{}, nil)
	})
	var a assistantapi.Assistant
	if err := client.Do(t.Context(), "GET", "/api/assistants/"+string(id), nil, &a); err != nil {
		t.Fatal(err)
	}
	m.assistants = []assistant{{id: id, chat: a.ConversationID, name: a.Name, channel: "webui", loaded: true, composer: newComposer()}}
	m.selected = 0
	m.input = m.assistants[0].composer
	m.live.loading = false
	m.live.scheduled = false
	m.preferredAssistant = ""
	for _, name := range []string{"/settings", "/status", "/approval", "/permissions", "/tasks", "/plans", "/processes", "/files", "/channel"} {
		t.Run(name, func(t *testing.T) {
			load(m.openManagement(name, ""))
			for _, size := range [][2]int{{100, 30}, {42, 18}} {
				m.resize(size[0], size[1])
				if text := m.View(); text == "" || strings.Contains(text, "WebUI →") {
					t.Fatal("invalid management view")
				}
			}
		})
	}
	// Exercise a real workspace upload using the TUI form and version-checked transport.
	local := filepath.Join(t.TempDir(), "report.txt")
	original := []byte("终端验收文件\nversion 1\n")
	if err := os.WriteFile(local, original, 0600); err != nil {
		t.Fatal(err)
	}
	m.workspaceUploadForm(id, ".")
	formValue(t, &m, "local", local)
	load(m.manage.screen.localSubmit(&m, values(m.manage.screen)))
	m.manage.screen.rows[0].open(&m)
	cmd, _ = m.updateManaged(tea.KeyMsg{Type: tea.KeyEnter})
	result = cmd().(managedResult)
	if result.err != nil {
		t.Fatal(result.err)
	}
	m.updateManaged(result)
	var entry workspace.Entry
	if err := client.Do(t.Context(), "GET", workspacePath(id, "report.txt", "stat"), nil, &entry); err != nil {
		t.Fatal(err)
	}
	var downloaded bytes.Buffer
	if err := client.WorkspaceDownload(t.Context(), id, entry, &downloaded); err != nil || !bytes.Equal(downloaded.Bytes(), original) {
		t.Fatalf("workspace roundtrip: %v", err)
	}

	m.page = chatPage
	m.back = nil
	m.manage.stack = nil
	m.input.SetValue("请只回复：终端验收成功。")
	next, send := m.send()
	m = next.(model)
	if send == nil {
		t.Fatalf("send unavailable: %s", m.notice)
	}
	sent := send().(sentMsg)
	if sent.err != nil {
		t.Fatal(sent.err)
	}
	next, _, _ = m.updateLive(sent)
	m = next.(model)
	deadline := time.Now().Add(45 * time.Second)
	replied := false
	for time.Now().Before(deadline) {
		var messages core.Page[conversation.Message]
		if err := client.Do(t.Context(), "GET", "/api/conversations/"+string(a.ConversationID)+"/messages?order=desc&limit=20", nil, &messages); err != nil {
			t.Fatal(err)
		}
		for _, message := range messages.Items {
			if message.Sequence > sent.message.Sequence && message.Sender == conversation.Assistant && strings.Contains(message.Content.Text, "终端验收成功") {
				replied = true
			}
		}
		if replied {
			break
		}
		time.Sleep(time.Second)
	}
	if !replied {
		t.Fatal("local model did not publish the requested acceptance reply within 45 seconds")
	}
	var created task.Task
	if err := client.Do(t.Context(), "POST", "/api/tasks", task.CreateRequest{Mutation: mutation(0), AssistantID: id, Goal: "检查报告文件，只需说明文件内容", AcceptanceCriteria: []string{"回复内容摘要"}}, &created); err != nil {
		t.Fatal(err)
	}
	load(m.taskScreen(created.ID))
	if !strings.Contains(m.manage.screen.body, "检查报告") {
		t.Fatal("task details missing")
	}
	load(m.searchScreen(id, "终端"))
	fmt.Printf("TUI isolated Runtime accepted assistant %s; workspace bytes=%d; management screens verified\n", id, len(original))
}
