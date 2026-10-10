package debugtrace

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/persistence"
	"strings"
	"testing"
)

func TestSanitizeNestedJSONWithLeadingWhitespace(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{"space_object", ` {"api_key":"synthetic-private-value","note":"retained"}`},
		{"tab_array", "\t[{\"password\":\"synthetic-private-value\",\"note\":\"retained\"}]"},
		{"newline_object", "\n{\"Authorization\":\"synthetic-private-value\",\"note\":\"retained\"}"},
		{"mixed_whitespace", " \t\r\n{\"secret\":\"synthetic-private-value\",\"note\":\"retained\",\"number\":9007199254740993}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, status := sanitize(map[string]any{"Arguments": tc.text}, 4096)
			if status != "redacted" || strings.Contains(string(raw), "synthetic-private-value") {
				t.Fatalf("nested credential was not redacted: status=%s", status)
			}
			var out map[string]string
			if err := json.Unmarshal(raw, &out); err != nil {
				t.Fatal(err)
			}
			if !json.Valid([]byte(out["Arguments"])) || !strings.Contains(out["Arguments"], "retained") || !strings.Contains(out["Arguments"], "[REDACTED]") {
				t.Fatal("redaction damaged the nested JSON or ordinary content")
			}
			if tc.name == "mixed_whitespace" && !strings.Contains(out["Arguments"], "9007199254740993") {
				t.Fatal("redaction changed an integer")
			}
		})
	}
}

func TestSanitizePreservesBenignWhitespaceAndNestedLayers(t *testing.T) {
	for _, value := range []string{" \t\r\n", " ordinary text \n", " \t{ \"z\":2, \"a\":9007199254740993 }\n", "\n [1, 2, {\"note\":\"unchanged\"}] \t"} {
		raw, status := sanitize(value, 4096)
		var got string
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if status != "captured" || got != value {
			t.Fatal("benign content changed", status, got)
		}
		raw, status = sanitize(map[string]string{"password": "synthetic-private-value", "text": value}, 4096)
		var out map[string]string
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		if status != "redacted" || out["text"] != value || out["password"] != "[REDACTED]" {
			t.Fatal("sibling redaction changed benign text")
		}
	}
	child := ` {"secret":"synthetic-private-value","note":"retained"}`
	nested, _ := json.Marshal(map[string]string{"child": child})
	for _, text := range []string{" \n" + string(nested), " \t" + child + " trailing content"} {
		raw, status := sanitize(text, 4096)
		if status != "redacted" || strings.Contains(string(raw), "synthetic-private-value") || !strings.Contains(string(raw), "retained") {
			t.Fatal("nested or trailing JSON bypassed redaction", status)
		}
	}
	value := " \n{\"secret\":\"x\"}"
	original, _ := json.Marshal(value)
	if raw, status := sanitize(value, len(original)); status != "too_large" || len(raw) != 0 {
		t.Fatal("redaction growth bypassed payload limit", status)
	}
}

func TestCaptureRedactsWhitespaceJSONInSourceAndModelPayload(t *testing.T) {
	s, p, _ := fixture(t)
	text := " \t\n{\"api_key\":\"synthetic-private-value\",\"note\":\"retained\"}"
	if err := s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "im.conversations", "owner/chat", map[string]string{"ID": "chat", "OwnerID": "owner", "PeerID": "assistant"}); err != nil {
			return err
		}
		return tx.Put(ctx, "im.messages", "owner/message", map[string]any{"ID": "message", "ConversationID": "chat", "Sender": "user", "Content": map[string]string{"Text": text}})
	}); err != nil {
		t.Fatal(err)
	}
	event := map[string]string{"Kind": "user_message", "OwnerID": "owner", "AssistantID": "assistant", "MessageID": "message"}
	ctx := s.Begin(t.Context(), "owner", "assistant", "event", "execution", "user_message", 1, event)
	s.Record(ctx, "model.request", "succeeded", "operation", map[string]string{"Body": text})
	s.Finish(ctx, "succeeded", "")
	runs, err := s.Runs(t.Context(), p, Query{})
	if err != nil || len(runs.Items) != 1 {
		t.Fatal("missing recording", err)
	}
	id := runs.Items[0].ID
	for _, step := range []int{1, 2} {
		payload, err := s.Detail(t.Context(), p, id, step)
		if err != nil {
			t.Fatal(err)
		}
		if payload.Status != "redacted" || strings.Contains(string(payload.Data), "synthetic-private-value") || !strings.Contains(string(payload.Data), "retained") {
			t.Fatalf("step %d retained unsafe source/payload", step)
		}
	}
	trajectory, err := s.Trajectory(t.Context(), p, TrajectoryQuery{})
	if err != nil || len(trajectory.Runs) != 1 {
		t.Fatal("missing trajectory", err)
	}
	source := trajectory.Runs[0].Source
	if source == nil || source.Status != "redacted" || strings.Contains(source.Text, "synthetic-private-value") || !strings.Contains(source.Text, "retained") {
		t.Fatal("unsafe trajectory source")
	}
}
