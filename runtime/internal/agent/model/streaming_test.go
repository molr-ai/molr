package model

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/tests/testkit"
)

func TestStreamingTimingToolsReasoningAndUsage(t *testing.T) {
	body := ": heartbeat\r\n\r\n" +
		`data: {"id":"s","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}` + "\n\n" +
		`data: {"id":"s","choices":[{"index":0,"delta":{"reasoning_content":"think"}}]}` + "\n\n" +
		`data: {"id":"s","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call","type":"function","function":{"name":"read","arguments":"{"}}]}}]}` + "\n\n" +
		`data: {"id":"s","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
		`data: {"id":"s","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":60},"prompt_cache_hit_tokens":60,"prompt_cache_miss_tokens":40}}` + "\n\ndata: [DONE]\n\n"
	start := time.Unix(1, 0)
	tick := start
	now := func() time.Time { tick = tick.Add(100 * time.Millisecond); return tick }
	raw, normalized, timing, err := readChatStream(OpenAIChat, strings.NewReader(body), start, now)
	testkit.RequireNoError(t, err)
	if string(raw) != body || timing.FirstTokenMs == nil || *timing.FirstTokenMs != 100 || timing.GenerationMs == nil || *timing.GenerationMs != 200 {
		t.Fatalf("timing=%+v", timing)
	}
	parsed, err := (deepSeekChatAdapter{}).Decode(normalized, reasoningProvider())
	testkit.RequireNoError(t, err)
	if parsed.Usage.InputTokens != 100 || parsed.Usage.CachedTokens != 60 || parsed.Usage.OutputTokens != 20 || len(parsed.Calls) != 1 || string(parsed.Calls[0].Arguments) != "{}" || !strings.Contains(string(parsed.State.Payload), "think") {
		t.Fatalf("completion=%+v", parsed)
	}
}

func TestStreamingMissingEvidenceAndSingleChunk(t *testing.T) {
	chunk := `data: {"id":"s","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}` + "\n\n"
	for _, body := range []string{chunk, "data: [DONE]\n\n", "data: broken\n\n", strings.Repeat("x", (8<<20)+2)} {
		if _, _, _, err := readChatStream(OpenAIChat, strings.NewReader(body), time.Now(), time.Now); err == nil {
			t.Fatal("accepted incomplete evidence")
		}
	}
	_, _, timing, err := readChatStream(OpenAIChat, strings.NewReader(chunk+"data: [DONE]\n\n"), time.Now(), time.Now)
	testkit.RequireNoError(t, err)
	if timing.FirstTokenMs == nil || timing.GenerationMs != nil {
		t.Fatal("single buffered fragment must not invent decode speed")
	}
}

func TestStreamingKeepsTimingWithoutResultCache(t *testing.T) {
	f := newContractFixture(t)
	c := f.Client.(*client)
	body := `data: {"id":"s","choices":[{"index":0,"delta":{"content":"he"}}]}` + "\n\n" +
		`data: {"id":"s","choices":[{"index":0,"delta":{"content":"llo"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1}}` + "\n\ndata: [DONE]\n\n"
	calls := 0
	c.options.HTTPClient = &http.Client{Transport: responseTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	s, err := c.Generate(t.Context(), f.Request)
	testkit.RequireNoError(t, err)
	s.Close()

	receipt, err := c.receipts.ByOperation(t.Context(), f.Owner.OwnerID, f.Request.Execution.OperationID)
	testkit.RequireNoError(t, err)
	if calls != 1 || receipt.Timing == nil || receipt.Timing.FirstTokenMs == nil || !receipt.OutcomeKnown {
		t.Fatalf("calls=%d receipt=%+v", calls, receipt)
	}
}
