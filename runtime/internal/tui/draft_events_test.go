package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/runtimeclient"
)

func persistedModel(t testing.TB, count int) model {
	t.Helper()
	m := newModel()
	m.drafts = openDraftStore(t.TempDir(), "connection", "owner")
	t.Cleanup(m.drafts.close)
	m.assistants = make([]assistant, count)
	for i := range m.assistants {
		a := &m.assistants[i]
		a.id = core.ID(fmt.Sprintf("a%d", i))
		a.composer = newComposer()
		a.composer.SetValue(strings.Repeat("draft", 200))
		m.drafts.restored[a.id] = true
	}
	m.input = m.assistants[0].composer
	if err := m.persistDrafts(); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDraftEventsPersistOnlyAffectedAssistants(t *testing.T) {
	m := persistedModel(t, 3)
	before := m.drafts.last["a2"]
	m.assistants[2].composer.SetValue("unrelated")
	m.input.SetValue("edited")
	if saved, err := m.persistEventDrafts(transferTick{}, "a0"); saved || err != nil {
		t.Fatal("progress event saved drafts")
	}
	if saved, err := m.persistEventDrafts(tea.KeyMsg{Type: tea.KeyRunes}, "a0"); !saved || err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.drafts.last["a0"], "edited") || m.drafts.last["a2"] != before {
		t.Fatal("key did not isolate affected draft")
	}
	m.assistants[1].composer.SetValue("background receipt")
	if _, err := m.persistEventDrafts(sentMsg{id: "a1"}, "a0"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.drafts.last["a1"], "background receipt") {
		t.Fatal("background send was not saved")
	}
	m.selectAssistant(1)
	m.input.SetValue("selected draft")
	if _, err := m.persistEventDrafts(tea.KeyMsg{Type: tea.KeyEnter}, "a0"); err != nil {
		t.Fatal(err)
	}
	if m.drafts.selected != "a1" || !strings.Contains(m.drafts.last["a1"], "selected draft") {
		t.Fatal("selection was not persisted")
	}
}

func TestDraftPersistenceRetriesAndSavesBackgroundUpload(t *testing.T) {
	m := persistedModel(t, 2)
	m.input.SetValue("retry after failure")
	m.draftError = "temporary write failure"
	if saved, err := m.persistEventDrafts(transferTick{}, "a0"); !saved || err != nil {
		t.Fatal("idle event did not retry", err)
	}
	if !strings.Contains(m.drafts.last["a0"], "retry after failure") {
		t.Fatal("retry lost draft")
	}
	m.draftError = ""
	m.assistants[1].composer.SetValue("background upload draft")
	if _, err := m.persistEventDrafts(attachmentResult{assistant: "a1"}, "a0"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.drafts.last["a1"], "background upload draft") {
		t.Fatal("upload receipt did not save its assistant")
	}
	m.input.SetValue("management result")
	if _, err := m.persistEventDrafts(managedResult{}, "a0"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.drafts.last["a0"], "management result") {
		t.Fatal("management result did not save current draft")
	}
}

func TestDraftCapacityCountsAllNewBlobsInOneSave(t *testing.T) {
	m := persistedModel(t, 1)
	var used int64
	if err := filepath.WalkDir(m.drafts.root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			used += info.Size()
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ballast, err := os.Create(filepath.Join(m.drafts.root, "ballast"))
	if err != nil {
		t.Fatal(err)
	}
	if err = ballast.Truncate(draftCacheLimit - used - 5); err != nil {
		t.Fatal(err)
	}
	ballast.Close()
	for range 2 {
		m.assistants[0].attachments = append(m.assistants[0].attachments, draftAttachment{key: runtimeclient.NewKey(), data: []byte("abc"), digest: blobDigest([]byte("abc"))})
	}
	if err := m.persistDrafts(); err == nil {
		t.Fatal("multiple new blobs exceeded cache limit")
	}
	first, second := m.assistants[0].attachments[0].key, m.assistants[0].attachments[1].key
	if _, err := os.Stat(filepath.Join(m.drafts.root, "a0", first+".bin")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.drafts.root, "a0", second+".bin")); !os.IsNotExist(err) {
		t.Fatal("over-limit blob written")
	}
}

func TestComposerLayoutDoesNotChangeInputMode(t *testing.T) {
	m := newModel()
	m.input.SetValue("draft")
	m.live = &liveSession{}
	m.assistants[0].loaded = true
	m.assistants[0].channel = "weixin"
	m.resizeComposer()
	if m.input.Value() != "draft" || m.commandOnly() {
		t.Fatal("layout changed composer state")
	}
	m.syncComposerMode()
	if !m.commandOnly() || m.assistants[0].messageDraft.Value() != "draft" {
		t.Fatal("explicit mode transition lost draft")
	}
}

func BenchmarkDraftEventPersistence(b *testing.B) {
	for _, count := range []int{1, 100} {
		b.Run(fmt.Sprintf("assistants_%d", count), func(b *testing.B) {
			m := persistedModel(b, count)
			for _, all := range []bool{true, false} {
				b.Run(fmt.Sprintf("all_drafts_%t", all), func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						if all {
							if err := m.persistDrafts(); err != nil {
								b.Fatal(err)
							}
						} else {
							if _, err := m.persistEventDrafts(transferTick{}, "a0"); err != nil {
								b.Fatal(err)
							}
						}
					}
				})
			}
		})
	}
}
