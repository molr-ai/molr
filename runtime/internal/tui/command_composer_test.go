package tui

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"molr.ai/runtime/internal/im/conversation"
)

func TestBoundComposerCommandsAndDraftRestoration(t *testing.T) {
	var paths []string
	m := workflowModel(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		fmt.Fprint(w, `{}`)
	})
	m = typeText(m, "原始草稿 ")
	m = pasteText(m, strings.Repeat("材料", 60))
	original, caret := m.composerText(), composerCaret(m.input)
	m.assistants[0].attachments = []draftAttachment{{name: "saved.txt"}}
	m.assistants[0].channel = "weixin"
	m.resizeComposer()
	if !m.commandOnly() || m.input.Value() != "" || m.input.Height() != 1 || m.attachmentRows() != 0 {
		t.Fatal("channel binding did not switch to an empty command composer")
	}
	view := ansi.Strip(m.chatView())
	if !strings.Contains(view, "/ 输入命令") || strings.Count(view, "聊天入口：微信") != 1 || strings.Contains(view, "草稿已保留") || strings.Contains(view, "saved.txt") {
		t.Fatalf("unexpected command view: %s", view)
	}
	m = typeText(m, "com")
	m = press(m, tea.KeyTab)
	if m.input.Value() != "compress" {
		t.Fatalf("completion: %q", m.input.Value())
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if cmd == nil {
		t.Fatal("command did not execute")
	}
	next, _, _ = m.updateLive(cmd())
	m = next.(model)
	if len(paths) != 1 || paths[0] != "/api/assistants/a/compress" || m.input.Value() != "" {
		t.Fatal("command execution or clearing failed", paths)
	}
	m = typeText(m, "普通消息")
	next, cmd = m.send()
	m = next.(model)
	if cmd != nil || !strings.Contains(m.notice, "未知命令") || !strings.Contains(ansi.Strip(m.chatView()), "未知命令") {
		t.Fatal("unknown command sent or feedback hidden")
	}
	m = press(m, tea.KeyCtrlC)
	if m.input.Value() != "" || len(m.assistants[0].attachments) != 1 {
		t.Fatal("clearing command lost message attachments")
	}
	m = typeText(m, "/help")
	m = press(m, tea.KeyEnter)
	if m.page != managePage {
		t.Fatal("slash-prefixed help did not open")
	}
	m = press(m, tea.KeyEscape)
	m = typeText(m, "stop")
	m.selectAssistant(1)
	m = typeText(m, "B draft")
	m.selectAssistant(0)
	if m.input.Value() != "stop" || !m.commandOnly() {
		t.Fatal("switching assistants lost command")
	}
	m.assistants[0].channel = "webui"
	m.resizeComposer()
	if m.commandOnly() || m.composerText() != original || composerCaret(m.input) != caret || len(m.assistants[0].pastes) != 1 || len(m.assistants[0].attachments) != 1 {
		t.Fatal("unbinding did not restore message draft, caret, paste block and attachment")
	}
	if !strings.HasPrefix(ansi.Strip(m.composerView()), "› ") {
		t.Fatal("message prompt not restored")
	}
}

func TestCommandComposerBlocksMessagesAndMultilinePaste(t *testing.T) {
	m := workflowModel(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request", r.URL.Path) })
	for _, channel := range []string{"", "weixin", "telegram", "slack", "dingtalk", "feishu", "qq"} {
		m.assistants[0].channel = channel
		m.resizeComposer()
		m.resetComposer()
		m = pasteText(m, "compress\nstop")
		if m.input.Value() != "" || len(m.assistants[0].attachments) != 0 {
			t.Fatal("multiline paste accepted", channel)
		}
		m = typeText(m, "unknown")
		m = press(m, tea.KeyCtrlJ)
		if strings.Contains(m.input.Value(), "\n") {
			t.Fatal("command newline accepted")
		}
		next, cmd := m.send()
		m = next.(model)
		if cmd != nil {
			t.Fatal("ordinary message sent", channel)
		}
		m.resetComposer()
		if cmd := m.attachPath("sample.txt"); cmd != nil {
			t.Fatal("attachment allowed", channel)
		}
	}
	m.resetComposer()
	m.resize(42, 18)
	if rows := len(strings.Split(m.View(), "\n")); rows > 18 {
		t.Fatal("short screen overflow", rows)
	}
}

func TestCommandComposerReceiptsPreserveCurrentInput(t *testing.T) {
	m := workflowModel(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{}`) })
	m.input.SetValue("message in flight")
	m.assistants[0].channel = "weixin"
	m.resizeComposer()
	m = typeText(m, "compress")
	next, _, _ := m.updateLive(sentMsg{id: "a", text: "message in flight", message: conversation.Message{ID: "sent", Sequence: 1}})
	m = next.(model)
	if m.input.Value() != "compress" || m.assistants[0].messageDraft.Value() != "" {
		t.Fatal("message receipt cleared command or left sent draft")
	}
	next, cmd := m.send()
	m = next.(model)
	m.selectAssistant(1)
	next, _, _ = m.updateLive(cmd())
	m = next.(model)
	m.selectAssistant(0)
	if m.input.Value() != "" {
		t.Fatal("command receipt failed after assistant switch")
	}
	m.assistants[0].messageDraft.SetValue("/compress")
	m.assistants[0].channel = "webui"
	m.resizeComposer()
	next, _, _ = m.updateLive(commandResult{id: "a", input: "/compress", commandOnly: true})
	m = next.(model)
	if m.input.Value() != "/compress" {
		t.Fatal("late command receipt cleared restored message")
	}
}

func TestBoundCommandInputPreservesPersistentMessageDraft(t *testing.T) {
	root := t.TempDir()
	store := openDraftStore(root, "connection", "owner")
	defer store.close()
	m := workflowModel(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request", r.URL.Path) })
	m.drafts = store
	if err := store.restore(&m.assistants[0]); err != nil {
		t.Fatal(err)
	}
	m = typeText(m, "保留草稿 ")
	m.insertPasteBlock(strings.Repeat("材料", 80))
	original, caret := m.composerText(), composerCaret(m.input)
	m.assistants[0].channel = "weixin"
	m.resizeComposer()
	m = typeText(m, "compress")
	m.selectAssistant(1)
	if err := m.persistDrafts(); err != nil {
		t.Fatal(err)
	}
	store.close()
	restored := openDraftStore(root, "connection", "owner")
	defer restored.close()
	a := assistant{id: "a", composer: newComposer()}
	if err := restored.restore(&a); err != nil {
		t.Fatal(err)
	}
	if expandPasteBlocks(a.composer.Value(), a.pastes) != original || composerCaret(a.composer) != caret {
		t.Fatal("command input replaced persistent message draft")
	}
}
