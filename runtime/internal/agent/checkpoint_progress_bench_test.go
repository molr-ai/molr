package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

// Measure the entire progress save, including its reads and transaction, on
// SQLite. Each tool-step iteration starts from the same history size.
func BenchmarkCheckpointProgress(b *testing.B) {
	for _, size := range []struct{ messages, bytes int }{{8, 128}, {128, 2048}, {512, 2048}} {
		for _, mode := range []string{"unchanged", "forced", "tools16"} {
			b.Run(fmt.Sprintf("messages=%d/bytes=%d/%s", size.messages, size.bytes, mode), func(b *testing.B) {
				db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "progress.db"))
				if err != nil {
					b.Fatal(err)
				}
				defer db.Close()
				e := NewRuntime(db, Options{})
				turn := Turn{Principal: core.Principal{OwnerID: "owner", ExecutionID: "execution"}, Event: event.AssistantEvent{ID: "event", OwnerID: "owner", AssistantID: "assistant", Kind: event.ResumeRequested, CreatedAt: time.Now()}}
				history := make([]model.Input, size.messages)
				for i := range history {
					history[i] = model.Input{Role: model.User, Text: fmt.Sprintf("%d: %s", i, strings.Repeat("x", size.bytes))}
				}
				p := turnProgress{Inputs: history}
				if err := e.saveProgress(b.Context(), turn, &p, progressIfChanged); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if mode != "tools16" {
						if err := e.saveProgress(b.Context(), turn, &p, progressSaveMode(mode == "forced")); err != nil {
							b.Fatal(err)
						}
						continue
					}
					p.Inputs = append([]model.Input(nil), history...)
					for step := range 16 {
						call := model.ToolCall{ID: fmt.Sprintf("call-%d", step), Name: "file_read", Arguments: json.RawMessage(`{"path":"example.txt"}`)}
						response := model.Input{Role: model.Assistant, Calls: []model.ToolCall{call}}
						p.Step, p.NextTool, p.Response = step, 0, &response
						p.Inputs = append(p.Inputs, response)
						if err := e.saveProgress(b.Context(), turn, &p, progressAlways); err != nil {
							b.Fatal(err)
						}
						p.NextTool = 1
						p.Inputs = append(p.Inputs, model.Input{Role: model.Tool, ReplyTo: call.ID, ToolOperationID: stableID(string(turn.Event.ID), "tool", call.ID), Text: `{"text":"tool result"}`})
						if err := e.saveProgress(b.Context(), turn, &p, progressIfChanged); err != nil {
							b.Fatal(err)
						}
					}
				}
			})
		}
	}
}
