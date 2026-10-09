package debugtrace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"molr.ai/runtime/internal/contextstore"
	"molr.ai/runtime/internal/persistence"
)

func TestModelDetailProjectsSharedContextAndPrunesReferences(t *testing.T) {
	s, p, now := fixture(t)
	s.options.MaxRuns = 1
	inputs := []map[string]any{{"Role": "user", "Text": "input"}, {"Role": "assistant", "Text": "shared model answer", "Calls": []any{}, "ProviderState": map[string]any{"Payload": map[string]any{"reasoning_content": "shared reasoning"}}}}
	ref := contextstore.Reference{OwnerID: p.OwnerID, AssistantID: "assistant", Kind: "checkpoint", ID: "assistant"}
	if err := s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return contextstore.Save(ctx, tx, ref, map[string]any{"Inputs": inputs})
	}); err != nil {
		t.Fatal(err)
	}
	ctx := s.Begin(t.Context(), p.OwnerID, "assistant", "event", "execution", "user_message", 1, nil)
	s.Record(ctx, "model.complete", "succeeded", "op", map[string]any{"Inputs": inputs, "Receipt": map[string]any{"ID": "receipt"}})
	s.Finish(ctx, "succeeded", "")
	runs, err := s.Runs(t.Context(), p, Query{})
	if err != nil || len(runs.Items) != 1 {
		t.Fatal(runs, err)
	}
	id := runs.Items[0].ID
	run, err := s.Get(t.Context(), p, id)
	if err != nil {
		t.Fatal(err)
	}
	if run.Steps[1].Preview != "shared model answer" {
		t.Fatal("summary not retained", run.Steps[1])
	}
	payload, err := s.Detail(t.Context(), p, id, 2)
	if err != nil || !strings.Contains(string(payload.Data), `"Text":"shared model answer"`) || !strings.Contains(string(payload.Data), "shared reasoning") || strings.Contains(string(payload.Data), `"Inputs"`) {
		t.Fatal("bad output projection", string(payload.Data), err)
	}
	objects, err := persistence.Scan[struct {
		Kind string
		Data json.RawMessage
	}](t.Context(), s.store, "context.objects")
	if err != nil {
		t.Fatal(err)
	}
	matches := 0
	for _, obj := range objects {
		if strings.Contains(string(obj.Data), "shared model answer") {
			matches++
			if obj.Kind != "message" {
				t.Fatal("response duplicated outside message node", obj)
			}
		}
	}
	if matches != 1 {
		t.Fatal("output was not shared with checkpoint", matches)
	}
	*now = now.Add(1)
	next := s.Begin(t.Context(), p.OwnerID, "assistant", "next", "next", "user_message", 1, nil)
	s.Finish(next, "succeeded", "")
	if _, err = s.Get(t.Context(), p, id); !persistence.Missing(err) {
		t.Fatal("old run retained", err)
	}
	for {
		n, err := contextstore.Collect(t.Context(), s.store, 100)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	if err := s.store.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		var value json.RawMessage
		return contextstore.Load(ctx, tx, ref, &value)
	}); err != nil {
		t.Fatal("workbench pruning destroyed checkpoint", err)
	}
}
