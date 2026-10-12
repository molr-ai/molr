package memory

import (
	"context"
	"molr.ai/runtime/internal/core"
	"os"
	"path/filepath"
	"testing"
)

func TestOptionalMetadataDiagnosticsKeepRawTextAndProtection(t *testing.T) {
	for _, tc := range []struct {
		name, header         string
		diagnostic, readOnly bool
	}{
		{"custom", "scope: assistant\ncreated: yesterday\ncustom: anything", false, false},
		{"bad_state", "state: invented", true, false},
		{"bad_date", "created_at: yesterday", true, false},
		{"confirmed_bad_date", "confirmed: true\ncreated_at: yesterday", true, true},
		{"ambiguous_confirmation", "confirmed: maybe", true, true},
		{"invalid_tagged_boolean", "confirmed: !!bool invalid", true, true},
		{"null_confirmation", "confirmed: null", true, true},
		{"broken_yaml", "state: [broken", true, true},
		{"duplicate_confirmation", "confirmed: true\nconfirmed: false", true, true},
		{"merged_confirmation", "defaults: &defaults\n  confirmed: true\n<<: *defaults\ncreated_at: yesterday", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			files := NewFiles(FileOptions{JournalRoot: filepath.Join(root, "journal"), Roots: func(context.Context, core.ID, RootQuery) ([]Root, error) {
				return []Root{{Scope: Assistant, AssistantID: "assistant", Directory: filepath.Join(root, "owner", "work")}}, nil
			}})
			ref := Ref{Scope: Assistant, AssistantID: "assistant", Path: ".agents/memory/note.md"}
			filename := filepath.Join(root, "owner", "work", ref.Path)
			if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
				t.Fatal(err)
			}
			raw := []byte("---\n" + tc.header + "\n---\nKeep this useful note.\n")
			if err := os.WriteFile(filename, raw, 0600); err != nil {
				t.Fatal(err)
			}
			file, err := files.ReadFile(t.Context(), "owner", ref)
			if err != nil || file.Text != string(raw) || file.ReadOnly != tc.readOnly || (len(file.Diagnostics) > 0) != tc.diagnostic {
				t.Fatalf("snapshot %+v error %v", file, err)
			}
			after := []byte("Repaired useful note.\n")
			change := []FileChange{{Ref: ref, Before: raw, BeforeExists: true, After: after, AfterExists: true}}
			err = files.PrepareFileOperation(t.Context(), "owner", "repair", "digest", change, func() error { return nil })
			if tc.readOnly {
				if core.CodeOf(err) != core.Forbidden {
					t.Fatal("lost protection", err)
				}
				return
			}
			if err != nil {
				t.Fatal("could not repair", err)
			}
			if err = os.WriteFile(filename, after, 0600); err != nil {
				t.Fatal(err)
			}
			if err = files.CompleteFileOperation(t.Context(), "owner", "repair", func() error { return nil }); err != nil {
				t.Fatal(err)
			}
			if _, err = files.UndoFileOperation(t.Context(), "owner", "repair", func() error { return nil }); err != nil {
				t.Fatal(err)
			}
			restored, err := os.ReadFile(filename)
			if err != nil || string(restored) != string(raw) {
				t.Fatal("undo did not preserve original bytes", err)
			}
		})
	}
}
