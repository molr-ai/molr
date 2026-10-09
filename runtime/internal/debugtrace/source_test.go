package debugtrace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"molr.ai/runtime/internal/persistence"
)

func TestTriggerCaptureAndLegacySourceIsolation(t *testing.T) {
	s, p, _ := fixture(t)
	s.options.MaxPayloadBytes = 16384
	put := func(bucket, key string, value any) {
		t.Helper()
		if err := s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return tx.Put(ctx, bucket, key, value) }); err != nil {
			t.Fatal(err)
		}
	}
	put("im.conversations", "owner/chat", map[string]string{"ID": "chat", "OwnerID": "owner", "PeerID": "assistant"})
	message := map[string]any{"ID": "message", "ConversationID": "chat", "Sender": "user", "Content": map[string]any{"Text": strings.Repeat("正文", 600) + " Bearer secret-source-token", "AttachmentIDs": []string{"attachment"}}}
	put("im.messages", "owner/message", message)
	event := map[string]any{"Kind": "user_message", "OwnerID": "owner", "AssistantID": "assistant", "MessageID": "message"}
	ctx := s.Begin(t.Context(), "owner", "assistant", "event", "execution", "user_message", 1, event)
	s.Finish(ctx, "succeeded", "")
	page, err := s.Trajectory(t.Context(), p, TrajectoryQuery{})
	if err != nil || len(page.Runs) != 1 {
		t.Fatalf("trajectory: %+v %v", page, err)
	}
	run := page.Runs[0]
	if run.Source == nil || !run.Source.Truncated || run.Source.Status != "redacted" || run.Source.Origin != "capture" || len([]rune(run.Source.Text)) != 1001 {
		t.Fatalf("summary: %+v", run.Source)
	}
	payload, err := s.Detail(t.Context(), p, run.ID, 1)
	if err != nil || payload.Status != "redacted" || strings.Contains(string(payload.Data), "secret-source-token") {
		t.Fatalf("unsafe payload %s %v", payload.Data, err)
	}
	var data struct{ Source SourceContent }
	json.Unmarshal(payload.Data, &data)
	if len([]rune(data.Source.Text)) <= 1001 || len(data.Source.AttachmentIDs) != 1 {
		t.Fatalf("lost full source: %+v", data.Source)
	}
	// A captured snapshot must survive source modification or deletion.
	message["Content"] = map[string]string{"Text": "changed original"}
	put("im.messages", "owner/message", message)
	unchanged, _ := s.Detail(t.Context(), p, run.ID, 1)
	if strings.Contains(string(unchanged.Data), "changed original") {
		t.Fatal("snapshot followed later edits")
	}
	// Simulate a recording from before source snapshots were introduced.
	saved, err := s.Get(t.Context(), p, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	saved.Source = nil
	put("debug.runs", persistence.Key(p.OwnerID, run.ID), saved)
	saved.Steps = nil
	put("debug.summaries", persistence.Key(p.OwnerID, run.ID), saved)
	payload.Data, _ = json.Marshal(map[string]any{"Event": event})
	payload.Status = "captured"
	if err := s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return savePayload(ctx, tx, saved, 1, payload) }); err != nil {
		t.Fatal(err)
	}
	legacy, err := s.Detail(t.Context(), p, run.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(legacy.Data, &data)
	if data.Source.Origin != "reference" || data.Source.Text != "changed original" {
		t.Fatalf("legacy source: %+v", data.Source)
	}
	page, err = s.Trajectory(t.Context(), p, TrajectoryQuery{})
	if err != nil || page.Runs[0].Source.Text != "changed original" {
		t.Fatalf("legacy summary: %+v %v", page, err)
	}
	// Ownership and assistant association must be checked even for legacy lookups.
	for _, conversation := range []map[string]string{
		{"ID": "chat", "OwnerID": "foreign", "PeerID": "assistant"},
		{"ID": "chat", "OwnerID": "owner", "PeerID": "other-assistant"},
	} {
		put("im.conversations", "owner/chat", conversation)
		legacy, err = s.Detail(t.Context(), p, run.ID, 1)
		if err != nil {
			t.Fatal(err)
		}
		json.Unmarshal(legacy.Data, &data)
		if data.Source.Status != "unavailable" || data.Source.Text != "" {
			t.Fatalf("foreign source disclosed: %+v", data.Source)
		}
	}
	if err := s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Delete(ctx, "im.messages", "owner/message")
	}); err != nil {
		t.Fatal(err)
	}
	legacy, _ = s.Detail(t.Context(), p, run.ID, 1)
	json.Unmarshal(legacy.Data, &data)
	if data.Source.Status != "unavailable" {
		t.Fatalf("deleted source: %+v", data.Source)
	}
}

func TestPlanSourceVersionAndPayloadLimits(t *testing.T) {
	s, _, _ := fixture(t)
	plan := map[string]any{"ID": "plan", "OwnerID": "owner", "AssistantID": "assistant", "Kind": "assistant_plan", "Title": "Daily plan", "Instructions": "original instructions", "Version": 2}
	put := func() {
		t.Helper()
		if err := s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, "business.schedules", "owner/plan", plan)
		}); err != nil {
			t.Fatal(err)
		}
	}
	put()
	lookup := func(version int, origin string) SourceContent {
		t.Helper()
		event, _ := json.Marshal(map[string]any{"Kind": "schedule_due", "OwnerID": "owner", "AssistantID": "assistant", "ScheduleID": "plan", "ScheduleVersion": version})
		var source SourceContent
		if err := s.store.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			source = s.sourceContent(ctx, tx, "owner", "assistant", event, origin)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return source
	}
	for _, version := range []int{0, 1, 3} {
		source := lookup(version, "reference")
		if source.Status != "version_mismatch" || source.Text != "" {
			t.Fatalf("wrong version shown: %+v", source)
		}
	}
	source := lookup(2, "reference")
	if source.Text != "original instructions" || source.Title != "Daily plan" {
		t.Fatalf("matching plan lost: %+v", source)
	}
	plan["Instructions"] = strings.Repeat("x", 4096)
	put()
	source = lookup(2, "capture")
	if source.Status != "too_large" || source.Text != "" {
		t.Fatalf("limit bypass: %+v", source)
	}
	plan["OwnerID"] = "foreign"
	put()
	source = lookup(2, "capture")
	if source.Status != "unavailable" {
		t.Fatalf("foreign plan: %+v", source)
	}
}

func TestBornTriggerHasLifecycleSource(t *testing.T) {
	s, p, _ := fixture(t)
	event := map[string]any{"Kind": "born", "OwnerID": "owner", "AssistantID": "assistant"}
	ctx := s.Begin(t.Context(), "owner", "assistant", "birth", "execution", "born", 1, event)
	s.Finish(ctx, "succeeded", "")
	page, err := s.Trajectory(t.Context(), p, TrajectoryQuery{})
	if err != nil || len(page.Runs) != 1 {
		t.Fatalf("trajectory: %+v %v", page, err)
	}
	source := page.Runs[0].Source
	if source == nil || source.Kind != "born" || source.ID != "assistant" || source.Status != "captured" || source.Text == "" {
		t.Fatalf("birth source: %+v", source)
	}
}
