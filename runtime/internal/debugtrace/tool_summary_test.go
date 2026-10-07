package debugtrace

import (
	"encoding/json"
	"strings"
	"testing"

	"molr.ai/runtime/internal/core"
)

func TestToolSummaryUsesSanitizedFullCaptureBeforePreviewTruncation(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := s.Begin(t.Context(), p.OwnerID, "assistant", "event", "execution", "user_message", 1, nil)
	body := strings.Repeat("消息正文", 90) + " Bearer PRIVATE_TOKEN"
	s.Record(ctx, "tool.start", "started", "publish", map[string]any{"Name": "conversation_publish", "Arguments": map[string]any{"conversation_id": "chat", "text": body}})
	items := []any{map[string]string{"Text": strings.Repeat("历史消息", 100)}, map[string]string{"Text": "第二条"}}
	s.Record(ctx, "tool.complete", "succeeded", "read", map[string]any{"Name": "conversation_read", "Result": map[string]any{"Content": map[string]any{"Items": items}}})
	s.Record(ctx, "tool.start", "started", "secret", map[string]any{"Name": "shell_execute", "Arguments": map[string]any{"script": "curl -H 'Authorization: Bearer PRIVATE_TOKEN' https://example.com"}})
	page, err := s.Trajectory(t.Context(), p, TrajectoryQuery{Query: Query{AssistantID: "assistant", Page: core.PageRequest{Limit: 50}}})
	if err != nil {
		t.Fatal(err)
	}
	byOperation := map[core.ID]Step{}
	for _, entry := range page.Items {
		byOperation[entry.Step.OperationID] = entry.Step
		encoded, _ := json.Marshal(entry.Step.Summary)
		if strings.Contains(string(encoded), "PRIVATE_TOKEN") {
			t.Fatal("summary leaked unsanitized content", string(encoded))
		}
	}
	publish := byOperation["publish"].Summary
	if publish == nil || publish.Text != strings.Repeat("消息正文", 60)+"…" {
		t.Fatalf("long message must be extracted before truncating: %+v", publish)
	}
	read := byOperation["read"].Summary
	if read == nil || read.Kind != "messages" || read.Count == nil || *read.Count != 2 {
		t.Fatalf("must count full result items: %+v", read)
	}
	if byOperation["secret"].Summary == nil {
		t.Fatal("missing sanitized command summary")
	}
}

func TestToolSummarySemanticInputsAndResultCounts(t *testing.T) {
	for _, tc := range []struct {
		name, kind, state, payload, text, summaryKind string
		count                                         int
	}{
		{"read limit is not result", "tool.start", "started", `{"Name":"conversation_read","Arguments":{"limit":50}}`, "", "", -1},
		{"empty read", "tool.complete", "succeeded", `{"Name":"conversation_read","Result":{"Content":{"Items":[]}}}`, "", "messages", 0},
		{"missing result", "tool.complete", "succeeded", `{"Name":"conversation_read","Result":{"Content":{}}}`, "", "", -1},
		{"failed read", "tool.complete", "failed", `{"Name":"conversation_read","Result":{"Content":{"Items":[{}]}}}`, "", "", -1},
		{"write path, not body", "tool.start", "started", `{"Name":"file_write","Arguments":{"path":"/work/report.md","text":"very large body"}}`, "/work/report.md", "text", -1},
		{"command", "tool.start", "started", `{"Name":"shell_execute","Arguments":{"script":"pwd\nls","compute_id":"id"}}`, "pwd ls", "text", -1},
		{"file query", "tool.start", "started", `{"Name":"file_search","Arguments":{"pattern":"项目约定","path":"/work/.agents/memory"}}`, "项目约定", "text", -1},
		{"attachment only", "tool.start", "started", `{"Name":"conversation_publish","Arguments":{"attachments":[{"path":"a"},{"path":"b"}]}}`, "", "attachments", 2},
		{"card without title", "tool.complete", "succeeded", `{"Name":"conversation_publish","Result":{"Content":{"Content":{"Card":{"Kind":"task","TargetID":"opaque"},"AttachmentIDs":null}}}}`, "", "", -1},
		{"card", "tool.start", "started", `{"Name":"conversation_publish","Arguments":{"card":{"title":"项目计划","target_id":"opaque"}}}`, "项目计划", "text", -1},
		{"completed publish without start", "tool.complete", "succeeded", `{"Name":"conversation_publish","Result":{"Content":{"Content":{"Text":"已完成报告"}}}}`, "已完成报告", "text", -1},
		{"plan title", "tool.start", "started", `{"Name":"plan_manage","Arguments":{"action":"create","title":"整理周报"}}`, "整理周报", "text", -1},
		{"unknown tool query", "tool.start", "started", `{"Name":"plugin_search","Arguments":{"query":"天气","token":"[REDACTED]"}}`, "天气", "text", -1},
		{"unavailable payload", "tool.start", "started", ``, "", "", -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := toolSummary(tc.kind, tc.state, json.RawMessage(tc.payload))
			if tc.summaryKind == "" {
				if got != nil {
					t.Fatalf("invented summary: %+v", got)
				}
				return
			}
			if got == nil || got.Kind != tc.summaryKind || got.Text != tc.text {
				t.Fatalf("unexpected summary: %+v", got)
			}
			if tc.count >= 0 && (got.Count == nil || *got.Count != tc.count) {
				t.Fatalf("unexpected count: %+v", got)
			}
		})
	}
}
