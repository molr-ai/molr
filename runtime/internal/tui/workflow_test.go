package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/runtimeclient"
	"molr.ai/runtime/internal/storage/files"
)

func workflowModel(t *testing.T, handler http.HandlerFunc) model {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, _ := runtimeclient.New(server.URL, "")
	m := newConnectedModel(t.Context(), client, make(chan runtimeclient.Event))
	m.assistants = []assistant{{id: "a", chat: "chat", loaded: true, channel: "webui", name: "A", composerState: composerState{composer: newComposer()}}, {id: "b", chat: "other", loaded: true, channel: "webui", name: "B", composerState: composerState{composer: newComposer()}}}
	m.notice = ""
	return m
}

// Run one asynchronous transfer result, leaving its progress timer and refresh
// scheduler to the event loop. No live subscription is needed for these tests.
func transferResult(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("missing transfer command")
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		return batch[0]()
	}
	return msg
}

func TestLongPasteUploadsOriginalAndSendsOnlyAttachment(t *testing.T) {
	raw := strings.Repeat("超长材料\t原文\r\n", 1100)
	var uploaded []byte
	var sends []conversation.SendUserRequest
	m := workflowModel(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/notifications":
			fmt.Fprint(w, `{"Items":[]}`)
			return
		case "/api/files":
			uploaded, _ = io.ReadAll(r.Body)
			sum := sha256.Sum256(uploaded)
			json.NewEncoder(w).Encode(files.File{ID: "file", Size: int64(len(uploaded)), SHA256: hex.EncodeToString(sum[:]), State: files.Ready})
		case "/api/conversations/messages":
			var body conversation.SendUserRequest
			json.NewDecoder(r.Body).Decode(&body)
			sends = append(sends, body)
			if len(sends) == 1 {
				w.WriteHeader(503)
				fmt.Fprint(w, `{"code":"unavailable"}`)
				return
			}
			json.NewEncoder(w).Encode(conversation.Message{ID: "message", Sequence: 1, Sender: conversation.User, Content: body.Content})
		default:
			t.Error("unexpected request", r.URL.Path)
		}
	})
	m.input.SetValue("请分析附件")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(raw), Paste: true})
	m = next.(model)
	if m.input.Value() != "请分析附件" || len(m.assistants[0].attachments) != 1 || !strings.Contains(m.View(), "粘贴文本-") || strings.Contains(m.View(), "[粘贴了") {
		t.Fatal("paste was inserted into message text")
	}
	if _, send := m.send(); send != nil {
		t.Fatal("sent before upload completed")
	}
	m.selectAssistant(1)
	m.input.SetValue("B draft")
	next, _ = m.Update(transferResult(t, cmd))
	m = next.(model)
	if string(uploaded) != raw || m.input.Value() != "B draft" || m.assistants[0].attachments[0].file.ID != "file" {
		t.Fatal("upload altered bytes or switched drafts")
	}
	m.selectAssistant(0)
	next, cmd = m.send()
	m = next.(model)
	next, _, _ = m.updateLive(cmd())
	m = next.(model)
	if len(m.assistants[0].attachments) != 1 || m.input.Value() != "请分析附件" {
		t.Fatal("send failure lost draft")
	}
	next, cmd = m.send()
	m = next.(model)
	next, _, _ = m.updateLive(cmd())
	m = next.(model)
	if len(sends) != 2 || sends[0].Mutation.IdempotencyKey != sends[1].Mutation.IdempotencyKey || sends[1].Content.Text != "请分析附件" || len(sends[1].Content.AttachmentIDs) != 1 || len(m.assistants[0].attachments) != 0 || m.input.Value() != "" {
		t.Fatal("attachment retry/send contract")
	}
}

func TestLongInputLimitsPreviewAndShortScreen(t *testing.T) {
	m := newModel()
	raw := strings.Repeat("一行\n", 200)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(raw), Paste: true})
	m = next.(model)
	if string(m.assistants[0].attachments[0].data) != raw || m.input.Value() != "" {
		t.Fatal("long paste lost")
	}
	m.input.CharLimit = 0 // Simulate a restored over-limit draft.
	m.input.SetValue(strings.Repeat("中", 4001))
	_, cmd := m.send()
	if cmd != nil {
		t.Fatal("oversized body sent")
	}
	m = press(m, tea.KeyCtrlT)
	if m.input.Value() != "" || len(m.assistants[0].attachments) != 2 {
		t.Fatal("convert body failed")
	}
	m.input.SetValue("第一行\n第二行\n第三行\n第四行\n第五行")
	for len(m.assistants[0].attachments) < 5 {
		m.addAttachment("sample", []byte("sample"), false)
	}
	m.resize(42, 18)
	if h := lipgloss.Height(m.View()); h > 18 {
		t.Fatalf("short screen overflow: %d\n%s", h, m.View())
	}
	m = press(m, tea.KeyCtrlE)
	m = press(m, tea.KeyEnter)
	if m.page != attachmentsPage || !strings.Contains(m.files.preview, "一行") {
		t.Fatal("paste preview unavailable")
	}
	m = press(m, tea.KeyEscape)
	if m.page != attachmentsPage || m.files.preview != "" {
		t.Fatal("preview escape lost page")
	}
	m = typeText(m, "d")
	if len(m.assistants[0].attachments) != 4 {
		t.Fatal("remove failed")
	}
}

func TestCommandsControlAndChannelLock(t *testing.T) {
	var paths []string
	var expedited agent.ExpediteRequest
	m := workflowModel(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/expedite") {
			json.NewDecoder(r.Body).Decode(&expedited)
			fmt.Fprint(w, `{"State":"waiting_tool"}`)
		} else {
			fmt.Fprint(w, `{}`)
		}
	})
	m.assistants[0].status = agent.Status{State: agent.Working, TriggerMessageID: "trigger", InterruptedThroughSequence: 2}
	m.assistants[0].messages = []message{{id: "trigger", role: "user", sequence: 3}, {id: "followup", role: "user", sequence: 4}}
	for _, input := range []string{"/stop", "/压缩", "/加急"} {
		m.input.SetValue(input)
		next, cmd := m.send()
		m = next.(model)
		if cmd == nil {
			t.Fatal(input)
		}
		next, _, _ = m.updateLive(cmd())
		m = next.(model)
		if m.input.Value() != "" {
			t.Fatal("command not consumed")
		}
	}
	if len(paths) != 3 || expedited.ThroughSequence != 4 || expedited.ConversationID != "chat" || expedited.Mutation.IdempotencyKey == "" {
		t.Fatal(paths, expedited)
	}
	m.input.SetValue("/com")
	m = press(m, tea.KeyTab)
	if m.input.Value() != "/compress" {
		t.Fatal("completion")
	}
	m.input.SetValue("/compress invalid")
	if _, cmd := m.send(); cmd != nil {
		t.Fatal("invalid command sent")
	}
	m.assistants[0].channel = "weixin"
	m.input.SetValue("must remain")
	next, cmd := m.send()
	m = next.(model)
	if cmd != nil || !strings.Contains(m.notice, "微信") || m.input.Value() != "must remain" {
		t.Fatal("channel lock")
	}
	before := len(m.assistants[0].attachments)
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.Repeat("x", 5000)), Paste: true})
	m = next.(model)
	if cmd != nil || len(m.assistants[0].attachments) != before {
		t.Fatal("upload allowed in external channel")
	}
}

func TestPermissionCommandsPreflightBatchAndExactScope(t *testing.T) {
	expiry := time.Now().Add(time.Hour)
	q := policy.PermissionRequest{ID: "p", AssistantID: "a", Version: 7, State: "pending", Input: policy.PermissionRequestInput{ResourceID: "disk", ResourceKind: "storage", Actions: []policy.Action{policy.Read}, PathPrefixes: []string{"/reports"}, ExpiresAt: &expiry}}
	second := q
	second.ID = "q"
	current := []policy.PermissionRequest{q, second}
	var resolutions []policy.PermissionResolution
	m := workflowModel(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(current)
			return
		}
		var resolution policy.PermissionResolution
		json.NewDecoder(r.Body).Decode(&resolution)
		resolutions = append(resolutions, resolution)
		if resolution.RequestID == "q" {
			w.WriteHeader(409)
			fmt.Fprint(w, `{"code":"conflict"}`)
			return
		}
		fmt.Fprint(w, `{}`)
	})
	m.assistants[0].requests = []policy.PermissionRequest{q, second}
	m.input.SetValue("/权限审批 同意 全部")
	next, cmd := m.send()
	m = next.(model)
	result := cmd().(commandResult)
	if result.err == nil || !strings.Contains(result.err.Error(), "1/2") || len(resolutions) != 2 || resolutions[0].Mutation.ExpectedVersion != 7 || !resolutions[0].Scope.ReadOnly || resolutions[0].Scope.PathPrefixes[0] != "/reports" || !resolutions[0].ExpiresAt.Equal(expiry) {
		t.Fatal(result, resolutions)
	}
	next, _, _ = m.updateLive(result)
	m = next.(model)
	current[0].Version++
	m.input.SetValue("/permission reject p")
	_, cmd = m.send()
	result = cmd().(commandResult)
	if result.err == nil || len(resolutions) != 2 {
		t.Fatal("stale version accepted")
	}
}

func TestDownloadPreservesExistingAndVerifiesBeforePublish(t *testing.T) {
	data := []byte("downloaded content")
	sum := sha256.Sum256(data)
	m := workflowModel(t, func(w http.ResponseWriter, r *http.Request) { w.Write(data) })
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	os.WriteFile(path, []byte("old"), 0600)
	m.files = newFileDialog()
	m.files.saving = true
	m.files.path.SetValue(path)
	m.files.downloadFile = files.File{ID: "file", Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
	if cmd := m.startDownload(); cmd != nil || !m.files.overwrite {
		t.Fatal("overwrite not confirmed")
	}
	result := transferResult(t, m.startDownload()).(downloaded)
	if result.err != nil {
		t.Fatal(result.err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(data) {
		t.Fatal("download not published")
	}
	m.files.overwrite = false
	m.files.path.SetValue(filepath.Join(dir, "new.txt"))
	m.files.downloadFile.SHA256 = strings.Repeat("0", 64)
	result = transferResult(t, m.startDownload()).(downloaded)
	if result.err == nil {
		t.Fatal("invalid hash accepted")
	}
	if _, err := os.Stat(result.path); !os.IsNotExist(err) {
		t.Fatal("partial download published")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("temporary file leaked")
	}
}

func TestDownloadRejectsDestinationRemovedBeforeConfirmation(t *testing.T) {
	m := workflowModel(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("changed destination must be rejected before downloading")
	})
	dir := t.TempDir()
	target := filepath.Join(dir, "existing.txt")
	other := filepath.Join(dir, "keep.txt")
	for _, path := range []string{target, other} {
		if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m.files = newFileDialog()
	m.files.saving = true
	m.files.path.SetValue(target)
	m.files.downloadFile = files.File{ID: "file", Size: 12}
	if cmd := m.startDownload(); cmd != nil || !m.files.overwrite {
		t.Fatal("existing destination did not require confirmation")
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if cmd := m.startDownload(); cmd != nil {
		t.Fatal("removed destination started a download")
	}
	if m.files.notice != "下载失败：目标文件已变化，请重新确认" {
		t.Fatalf("unexpected notice: %s", m.files.notice)
	}
	if m.files.busy || m.files.cancel != nil || m.files.progress != nil || !m.files.saving || !m.files.overwrite || m.files.path.Value() != target || m.files.downloadFile.ID != "file" || m.files.downloadFile.Size != 12 {
		t.Fatal("rejected download changed dialog or transfer state")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("destination was recreated: %v", err)
	}
	data, err := os.ReadFile(other)
	if err != nil || string(data) != "original" {
		t.Fatalf("unrelated file changed: %q, %v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "keep.txt" {
		t.Fatalf("unexpected files after rejection: %v, %v", entries, err)
	}
}

func TestAttachmentOnlySendAndApprovalChildExpiry(t *testing.T) {
	var body conversation.SendUserRequest
	m := workflowModel(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		json.NewEncoder(w).Encode(conversation.Message{ID: "m", Content: body.Content})
	})
	m.assistants[0].attachments = []draftAttachment{{key: "key", file: files.File{ID: "file"}}}
	_, cmd := m.send()
	if cmd == nil {
		t.Fatal("attachment only send blocked")
	}
	cmd()
	if body.Content.Text != "" || len(body.Content.AttachmentIDs) != 1 {
		t.Fatal(body)
	}
	parent := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	child := parent.Add(24 * time.Hour)
	m.requestScreen(policy.PermissionRequest{Input: policy.PermissionRequestInput{ExpiresAt: &parent, Additional: []policy.PermissionRequestInput{{ExpiresAt: &child}}}})
	detail := m.manage.screen.body
	if !strings.Contains(detail, child.Local().Format("2006-01-02")) {
		t.Fatal("child expiry not shown")
	}
}

func TestAttachmentPageResizeAndDirectorySelection(t *testing.T) {
	m := workflowModel(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected transfer") })
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "long filename with spaces.txt"), []byte("fixture"), 0600)
	os.Mkdir(filepath.Join(dir, "child"), 0700)
	m.openFiles()
	m.readDirectory(dir)
	m.resize(42, 18)
	if lipgloss.Width(m.View()) > 42 || lipgloss.Height(m.View()) > 18 {
		t.Fatal("file picker resize overflow")
	}
	m = press(m, tea.KeyEnter)
	if m.files.directory != filepath.Join(dir, "child") {
		t.Fatal("directory selection failed")
	}
	m = press(m, tea.KeyBackspace)
	if m.files.directory != dir {
		t.Fatal("parent directory failed")
	}
	m.files.path.SetValue(filepath.Join(dir, "long"))
	m.completePath()
	if m.files.path.Value() != filepath.Join(dir, "long filename with spaces.txt") {
		t.Fatal("completion lost spaces")
	}
	m.files.path.SetValue(dir)
	m = press(m, tea.KeyCtrlL)
	if !m.files.pathFocused {
		t.Fatal("path not focused")
	}
	m = press(m, tea.KeyEscape)
	m.addAttachment("", []byte(strings.Repeat("连续文本", 2000)), true)
	m.openAttachments(true)
	m = press(m, tea.KeyEnter)
	m.resize(42, 18)
	if lipgloss.Width(m.View()) > 42 || lipgloss.Height(m.View()) > 18 {
		t.Fatal("preview resize overflow")
	}
}

func TestComposerLimitAndCumulativePasteBecomeAttachment(t *testing.T) {
	m := newModel()
	m.input.SetValue(strings.Repeat("中", 3999))
	m = typeText(m, "文")
	if len([]rune(m.input.Value())) != 4000 {
		t.Fatal("limit boundary")
	}
	m = typeText(m, "不截断")
	if len([]rune(m.input.Value())) != 4000 || !strings.Contains(m.notice, "4000") {
		t.Fatal("typed limit feedback")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("完整粘贴"), Paste: true})
	m = next.(model)
	if len(m.assistants[0].attachments) != 1 || string(m.assistants[0].attachments[0].data) != "完整粘贴" || len([]rune(m.input.Value())) != 4000 {
		t.Fatal("cumulative paste was truncated")
	}
}
