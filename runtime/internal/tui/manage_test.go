package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	assistantapi "molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/runtimeclient"
)

func applyManaged(t *testing.T, m *model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("missing command")
	}
	msg := cmd()
	if _, ok := msg.(managedResult); !ok {
		t.Fatalf("unexpected result %T", msg)
	}
	m.updateManaged(msg)
}
func formValue(t *testing.T, m *model, key, value string) {
	t.Helper()
	for i := range m.manage.screen.fields {
		if m.manage.screen.fields[i].key == key {
			m.manage.screen.fields[i].input.SetValue(value)
			return
		}
	}
	t.Fatalf("missing field %s", key)
}
func submitManaged(t *testing.T, m *model) {
	t.Helper()
	cmd, handled := m.updateManaged(tea.KeyMsg{Type: tea.KeyEnter})
	if !handled || cmd == nil {
		t.Fatalf("submit failed: %s", m.manage.screen.notice)
	}
	applyManaged(t, m, cmd)
}
func managedModel(t *testing.T, handler http.HandlerFunc) model {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := runtimeclient.New(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	m := newConnectedModel(t.Context(), c, make(chan runtimeclient.Event))
	m.assistants = []assistant{{id: "a", name: "A", chat: "chat", loaded: true, channel: "webui", composer: newComposer()}}
	return m
}

func TestManagementHireAndSettingsUseExistingAuthorization(t *testing.T) {
	var hires []assistantapi.HireRequest
	var updates []assistantapi.UpdateRequest
	m := managedModel(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/models":
			fmt.Fprint(w, `[{"ID":"model-ok","Name":"可用模型","ResourceID":"provider","Pricing":{}},{"ID":"model-denied","Name":"未授权模型","ResourceID":"provider","Pricing":{}}]`)
		case "/api/grants":
			fmt.Fprint(w, `{"Items":[{"ID":"g","ResourceID":"provider","Scope":{"ModelID":"model-ok"},"Actions":["read","execute"]}]}`)
		case "/api/assistants/a":
			fmt.Fprint(w, `{"ID":"a","Name":"原名","ModelID":"model-ok","Version":7,"Budget":{"Limit":{"Currency":"USD","Micros":2000000},"Period":"daily"}}`)
		case "/api/assistants":
			var q assistantapi.HireRequest
			if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
				t.Error(err)
			}
			hires = append(hires, q)
			fmt.Fprint(w, `{"ID":"new-assistant"}`)
		case "/api/assistants/update":
			var q assistantapi.UpdateRequest
			json.NewDecoder(r.Body).Decode(&q)
			updates = append(updates, q)
			fmt.Fprint(w, `{}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	applyManaged(t, &m, m.assistantForm(true))
	formValue(t, &m, "name", "终端助理")
	formValue(t, &m, "budget", "1.234567")
	submitManaged(t, &m)
	if len(hires) != 1 || hires[0].Budget.Limit.Micros != 1234567 || hires[0].Mutation.IdempotencyKey == "" || m.preferredAssistant != "new-assistant" || m.page != chatPage {
		t.Fatalf("hire did not complete %+v", hires)
	}
	applyManaged(t, &m, m.assistantForm(false))
	for _, f := range m.manage.screen.fields {
		if f.key == "model" && len(f.choices) != 1 {
			t.Fatalf("ungranted model offered: %v", f.choices)
		}
	}
	formValue(t, &m, "name", "已改名")
	submitManaged(t, &m)
	if len(updates) != 1 || updates[0].Mutation.ExpectedVersion != 7 || updates[0].ModelID != "model-ok" {
		t.Fatal("settings lost grant or concurrency boundary")
	}
}
func TestManagementSearchReadsExactMessageWithoutHistoryScan(t *testing.T) {
	m := managedModel(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search":
			if r.URL.Query().Get("source") != "messages" || r.URL.Query().Get("q") != "报告" {
				t.Error("invalid search")
			}
			fmt.Fprint(w, `{"Hits":{"Items":[{"AssistantID":"a","Object":{"Kind":"message","ID":"m"},"Title":"A","Snippet":"报告"}]}}`)
		case "/api/assistants/a":
			fmt.Fprint(w, `{"ConversationID":"chat","Name":"A"}`)
		case "/api/conversations/chat/messages":
			if r.URL.Query().Get("message_id") != "m" {
				t.Error("scanned chat instead of exact read")
			}
			fmt.Fprint(w, `{"Items":[{"ID":"m","Sender":"user","CreatedAt":"2026-01-01T00:00:00Z","Content":{"Text":"原始 **报告**"}}]}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	})
	applyManaged(t, &m, m.searchScreen("a", "报告"))
	applyManaged(t, &m, m.manage.screen.rows[0].open(&m))
	if !strings.Contains(m.manage.screen.body, "原始 **报告**") {
		t.Fatal("message source lost")
	}
	copied := ""
	m.writeClipboard = func(s string) error { copied = s; return nil }
	m.manage.screen.rows[0].open(&m)
	if copied != "原始 **报告**" {
		t.Fatal("copied rendering rather than source")
	}
	m.closeScreen()
	if m.manage.screen.title != "搜索结果" {
		t.Fatal("search location lost")
	}
}
func TestManagementApprovalFreezesGroupedScopeAndVersion(t *testing.T) {
	var sent []policy.PermissionResolution
	m := managedModel(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/permission-requests/resolve" {
			t.Fatalf("unexpected %s", r.URL.Path)
		}
		var q policy.PermissionResolution
		json.NewDecoder(r.Body).Decode(&q)
		sent = append(sent, q)
		if len(sent) == 1 {
			w.WriteHeader(503)
			fmt.Fprint(w, `{"code":"unavailable"}`)
		} else {
			fmt.Fprint(w, `{}`)
		}
	})
	until := time.Now().Add(time.Hour)
	q := policy.PermissionRequest{ID: "request", AssistantID: "a", Version: 9, Input: policy.PermissionRequestInput{ResourceID: "store", ResourceKind: "storage", Actions: []policy.Action{policy.Read}, PathPrefixes: []string{"/reports"}, ExpiresAt: &until, Additional: []policy.PermissionRequestInput{{ResourceID: "compute", ResourceKind: "compute", Actions: []policy.Action{policy.Execute}, Constraints: defaultConstraints("compute", false), ExpiresAt: &until}}}}
	m.requestScreen(q)
	m.manage.screen.rows[0].open(&m)
	m.assistants[0].requests = []policy.PermissionRequest{{ID: "new-request", Version: 20}}
	submitManaged(t, &m)
	if !strings.Contains(m.manage.screen.notice, "503") {
		t.Fatal("unknown result not retained")
	}
	submitManaged(t, &m)
	if len(sent) != 2 || sent[0].RequestID != "request" || sent[0].Mutation.ExpectedVersion != 9 || sent[0].Mutation.IdempotencyKey != sent[1].Mutation.IdempotencyKey || len(sent[0].Additional) != 1 || sent[0].Scope.PathPrefixes[0] != "/reports" {
		t.Fatalf("unsafe approval %+v", sent)
	}
}
func TestManagementTaskControlAndLatePageResults(t *testing.T) {
	var control task.ControlRequest
	m := managedModel(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tasks/t":
			fmt.Fprint(w, `{"ID":"t","AssistantID":"a","Goal":"完成报告","State":"running","Version":8}`)
		case "/api/tasks/t/revisions":
			fmt.Fprint(w, `{"Items":[]}`)
		case "/api/tasks/pause":
			json.NewDecoder(r.Body).Decode(&control)
			fmt.Fprint(w, `{}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	cmd := m.taskScreen("t")
	old := cmd()
	m.showScreen(manageScreen{title: "另一个页面"})
	m.updateManaged(old)
	if m.manage.screen.title != "另一个页面" {
		t.Fatal("late response replaced current page")
	}
	applyManaged(t, &m, m.taskScreen("t"))
	m.manage.screen.rows[0].open(&m)
	submitManaged(t, &m)
	if control.ID != "t" || control.Mutation.ExpectedVersion != 8 {
		t.Fatal("wrong task control target")
	}
}
func TestManagedFormsAndDetailsFitSmallTerminal(t *testing.T) {
	m := newModel()
	m.page = managePage
	m.resize(42, 18)
	m.manage.screen = manageScreen{title: "详情", body: strings.Repeat("第一段详情\n", 5) + strings.Repeat("后续详情\n", 100), rows: []manageRow{{"查看", func(*model) tea.Cmd { return nil }}}}
	before := m.managedView()
	m.updateManaged(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.managedView() == before {
		t.Fatal("detail cannot scroll independently of actions")
	}
	m.manage.screen = manageScreen{title: "表单", fields: constraintFields("base.", defaultConstraints("compute", false))}
	m.manage.screen.field = len(m.manage.screen.fields) - 1
	view := m.managedView()
	if !strings.Contains(view, "GPU") {
		t.Fatal("last field outside short screen")
	}
	m.manage.screen.dirty = true
	m.updateManaged(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.manage.screen.confirmLeave {
		t.Fatal("lost dirty form without confirmation")
	}
}
func TestPersistentDraftRestoresOriginalBytesAndPendingReceipt(t *testing.T) {
	root := t.TempDir()
	store := openDraftStore(root, "connection", "owner")
	defer store.close()
	m := newModel()
	m.assistants = m.assistants[:1]
	a := &m.assistants[0]
	a.id = "a"
	a.composer = newComposer()
	m.input = a.composer
	m.drafts = store
	if err := store.restore(a); err != nil {
		t.Fatal(err)
	}
	m.input.SetValue("前 ")
	m.input.CursorEnd()
	m.insertPasteBlock(strings.Repeat("原文", 80))
	m.addAttachment("notes.txt", []byte("原始\t附件\n"), true)
	a = &m.assistants[0]
	a.pending = pendingSend{text: m.composerText(), key: runtimeclient.NewKey(), attachments: []core.ID{"file"}}
	if err := m.persistDrafts(); err != nil {
		t.Fatal(err)
	}
	pending := a.pending.key
	original := m.composerText()
	key := a.attachments[0].key
	locked := openDraftStore(root, "connection", "owner")
	defer locked.close()
	if locked.err == nil {
		t.Fatal("second writer acquired lock")
	}
	other := openDraftStore(root, "connection", "different-owner")
	defer other.close()
	empty := assistant{id: "a", composer: newComposer()}
	if err := other.restore(&empty); err != nil || empty.composer.Value() != "" {
		t.Fatal("cross-account draft leak")
	}
	store.close()
	restored := openDraftStore(root, "connection", "owner")
	defer restored.close()
	next := assistant{id: "a", composer: newComposer()}
	if err := restored.restore(&next); err != nil {
		t.Fatal(err)
	}
	if expandPasteBlocks(next.composer.Value(), next.pastes) != original || next.pending.key != pending || next.attachments[0].key != key || string(next.attachments[0].data) != "原始\t附件\n" || next.pending.active {
		t.Fatal("draft retry identity or content changed")
	}
	info, err := os.Stat(filepath.Join(restored.root, "a", "draft.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("private draft permissions")
	}
}
func TestCorruptDraftIsNotOverwrittenAndClearPersists(t *testing.T) {
	s := openDraftStore(t.TempDir(), "connection", "owner")
	defer s.close()
	dir := filepath.Join(s.root, "a")
	os.MkdirAll(dir, 0700)
	file := filepath.Join(dir, "draft.json")
	os.WriteFile(file, []byte("broken"), 0600)
	a := assistant{id: "a", composer: newComposer()}
	if err := s.restore(&a); err == nil {
		t.Fatal("corruption ignored")
	}
	m := newModel()
	m.drafts = s
	m.assistants = []assistant{a}
	if m.persistDrafts() == nil {
		t.Fatal("corrupt file overwritten")
	}
	raw, _ := os.ReadFile(file)
	if string(raw) != "broken" {
		t.Fatal("lost corrupt draft")
	}
	fresh := openDraftStore(t.TempDir(), "connection", "owner")
	defer fresh.close()
	m.drafts = fresh
	fresh.restore(&m.assistants[0])
	m.input.SetValue("saved")
	if err := m.persistDrafts(); err != nil {
		t.Fatal(err)
	}
	m.clearComposerDraft()
	if err := m.persistDrafts(); err != nil {
		t.Fatal(err)
	}
	fresh.restored = map[core.ID]bool{}
	out := assistant{id: "a", composer: newComposer()}
	if err := fresh.restore(&out); err != nil || out.composer.Value() != "" {
		t.Fatal("cleared draft resurrected")
	}
}

func TestPendingSendKeepsOriginalPayloadAfterEditing(t *testing.T) {
	var text, key string
	m := managedModel(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Mutation core.Mutation
			Content  struct{ Text string }
		}
		json.NewDecoder(r.Body).Decode(&body)
		text, key = body.Content.Text, body.Mutation.IdempotencyKey
		fmt.Fprint(w, `{"ID":"sent","Sequence":1,"Sender":"user","Content":{"Text":"original"}}`)
	})
	m.assistants[0].pending = pendingSend{text: "original", key: "stable-retry"}
	m.input.SetValue("new draft")
	next, cmd := m.send()
	m = next.(model)
	if cmd != nil || !strings.Contains(m.notice, "未确认") {
		t.Fatal("unknown send replaced without review")
	}
	next, cmd = m.sendPending()
	m = next.(model)
	if cmd == nil {
		t.Fatal("cannot retry original")
	}
	next, _, _ = m.updateLive(cmd())
	m = next.(model)
	if text != "original" || key != "stable-retry" || m.input.Value() != "new draft" {
		t.Fatal("retry changed payload or cleared later input")
	}
}
