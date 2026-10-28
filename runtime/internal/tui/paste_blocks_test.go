package tui

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"molr.ai/runtime/internal/im/conversation"
)

func pasteText(m model, text string) model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true})
	return next.(model)
}

func TestInlinePasteCollapsesAndSendsOriginalBody(t *testing.T) {
	var sent conversation.SendUserRequest
	m := workflowModel(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/conversations/messages" {
			t.Fatalf("unexpected upload/request %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&sent)
		json.NewEncoder(w).Encode(conversation.Message{ID: "m", Sender: conversation.User, Content: sent.Content})
	})
	text := "第一行\t原文\r\n第二行\n第三行"
	m = typeText(m, "前缀 ")
	m = pasteText(m, text)
	m = typeText(m, " 后缀")
	if len(m.assistants[0].attachments) != 0 || len(m.assistants[0].pastes) != 1 || strings.Contains(m.input.Value(), "\n") || !strings.Contains(m.input.Value(), "[粘贴了") {
		t.Fatal("paste not an inline block")
	}
	next, cmd := m.send()
	m = next.(model)
	result := cmd()
	if sent.Content.Text != "前缀 "+text+" 后缀" || len(sent.Content.AttachmentIDs) != 0 {
		t.Fatalf("wrong body: %q", sent.Content.Text)
	}
	next, _, _ = m.updateLive(result)
	m = next.(model)
	if m.input.Value() != "" || len(m.assistants[0].pastes) != 0 {
		t.Fatal("successful send left stale paste blocks")
	}
}

func TestPasteBlocksAreAtomicForCursorAndDeletion(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyBackspace, tea.KeyDelete, tea.KeyCtrlW, tea.KeyCtrlU} {
		t.Run(key.String(), func(t *testing.T) {
			m := newModel()
			m = typeText(m, "prefix ")
			m = pasteText(m, strings.Repeat("长", 101))
			m = typeText(m, " suffix")
			block := m.assistants[0].pastes[0]
			if key == tea.KeyDelete {
				m.setComposerCaret(block.start)
			} else {
				m.setComposerCaret(block.end)
			}
			m = press(m, key)
			if len(m.assistants[0].pastes) != 0 || strings.Contains(m.input.Value(), "粘贴") || strings.Contains(m.composerText(), "长") {
				t.Fatal("partial block remained", m.input.Value())
			}
		})
	}
	m := newModel()
	m = typeText(m, "before ")
	m = pasteText(m, strings.Repeat("x", 101))
	m = typeText(m, " after")
	block := m.assistants[0].pastes[0]
	m.setComposerCaret(block.end)
	m = press(m, tea.KeyLeft)
	if composerCaret(m.input) != block.start {
		t.Fatal("left entered a block")
	}
	m = press(m, tea.KeyRight)
	if composerCaret(m.input) != block.end {
		t.Fatal("right entered a block")
	}
}

func TestPasteBlockThresholdsIsolationAndExpandedLimit(t *testing.T) {
	m := newModel()
	m = pasteText(m, strings.Repeat("x", 100))
	if len(m.assistants[0].pastes) != 0 {
		t.Fatal("100 chars should remain editable")
	}
	m.clearComposerDraft()
	m = pasteText(m, "one\ntwo")
	if len(m.assistants[0].pastes) != 0 {
		t.Fatal("two lines should remain editable")
	}
	m.clearComposerDraft()
	first := strings.Repeat("same prefix", 10) + "A"
	second := strings.Repeat("same prefix", 10) + "B"
	m = pasteText(m, first)
	m = pasteText(m, second)
	if m.composerText() != first+second || len(m.assistants[0].pastes) != 2 {
		t.Fatal("identical labels mixed original text")
	}
	m.selectAssistant(1)
	m = typeText(m, "other draft")
	m.selectAssistant(0)
	if m.composerText() != first+second {
		t.Fatal("switch lost blocks")
	}
	m = press(m, tea.KeyBackspace)
	if m.composerText() != first {
		t.Fatal("deleted the wrong identical block")
	}
	m.clearComposerDraft()
	m = pasteText(m, strings.Repeat("中", 3999))
	m = typeText(m, "字")
	if len([]rune(m.composerText())) != 4000 {
		t.Fatal("limit did not count original text")
	}
	m = pasteText(m, "extra")
	if len(m.assistants[0].attachments) != 1 || len([]rune(m.composerText())) != 4000 || strings.Contains(attachmentLabel(m.assistants[0].attachments[0]), "[粘贴了") {
		t.Fatal("expanded limit or attachment presentation")
	}
}

func TestCtrlCClearsBodyBlocksAndUploadsBeforeQuitting(t *testing.T) {
	m := newModel()
	m = pasteText(m, strings.Repeat("x", 101))
	cancelled := false
	m.assistants[0].attachments = []draftAttachment{{name: "file.txt", busy: true, cancel: func() { cancelled = true }}}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(model)
	if cmd != nil || !cancelled || m.input.Value() != "" || len(m.assistants[0].attachments) != 0 || len(m.assistants[0].pastes) != 0 {
		t.Fatal("Ctrl+C did not clear draft")
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("empty Ctrl+C did not quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("not quit")
	}
	m = typeText(m, " ")
	if _, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd != nil {
		t.Fatal("whitespace draft quit")
	}
}

func TestClearDuringSendDoesNotClearANewIdenticalDraft(t *testing.T) {
	m := workflowModel(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(conversation.Message{ID: "m", Sender: conversation.User, Content: conversation.Content{Kind: conversation.Text, Text: "same"}})
	})
	m = typeText(m, "same")
	next, cmd := m.send()
	m = next.(model)
	m = press(m, tea.KeyCtrlC)
	m = typeText(m, "same")
	next, _, _ = m.updateLive(cmd())
	m = next.(model)
	if m.input.Value() != "same" {
		t.Fatal("late receipt erased new draft")
	}
}

func TestTypedLabelCharactersDoNotExpandOrDeleteAdjacentBlocks(t *testing.T) {
	m := newModel()
	text := strings.Repeat("same", 30)
	m = pasteText(m, text)
	m = typeText(m, "]")
	m = press(m, tea.KeyBackspace)
	if m.composerText() != text {
		t.Fatal("literal closing bracket deleted block")
	}
	m = press(m, tea.KeyCtrlHome)
	m = typeText(m, "[")
	if m.composerText() != "["+text || len(m.assistants[0].pastes) != 1 {
		t.Fatal("literal opening bracket entered block")
	}
	m = press(m, tea.KeyBackspace)
	if m.composerText() != text {
		t.Fatal("deleting literal opening bracket deleted block")
	}
}
