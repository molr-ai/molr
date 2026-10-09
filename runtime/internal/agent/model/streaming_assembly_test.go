package model

import (
	"encoding/json"
	"io"
	"net/http"

	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"molr.ai/runtime/internal/persistence"
)

func streamMessage(t *testing.T, completion []byte) map[string]json.RawMessage {
	t.Helper()
	var response struct {
		Choices []struct{ Message map[string]json.RawMessage }
	}
	if err := json.Unmarshal(completion, &response); err != nil || len(response.Choices) != 1 {
		t.Fatalf("completion decode: %s %v", completion, err)
	}
	return response.Choices[0].Message
}

func TestStreamingAssemblyManyFragments(t *testing.T) {
	for _, field := range []string{"content", "reasoning_content", "tools"} {
		t.Run(field, func(t *testing.T) {
			body, want := fragmentedStream(4096, field)
			raw, completion, _, err := readChatStream(strings.NewReader(body), time.Now(), time.Now)
			if err != nil || string(raw) != body {
				t.Fatalf("stream/evidence: %v", err)
			}
			message := streamMessage(t, completion)
			var got string
			if field == "tools" {
				var calls []wireCall
				if err := json.Unmarshal(message["tool_calls"], &calls); err != nil || len(calls) != 1 {
					t.Fatalf("calls: %v %v", calls, err)
				}
				var args struct{ Text string }
				if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
					t.Fatal(err)
				}
				got = args.Text
				if calls[0].ID != "call" || calls[0].Function.Name != "publish" {
					t.Fatal(calls)
				}
			} else if err := json.Unmarshal(message[field], &got); err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("fragmented %s differs: bytes=%d want=%d", field, len(got), len(want))
			}
		})
	}
}

func TestStreamingAssemblyPreservesUnicodeInterleavingAndFieldReplacement(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"s","choices":[{"index":0,"delta":{"content":"你","reasoning_content":"思","refusal":"不","vendor":"discard","tool_calls":[{"index":1,"id":"b","type":"function","function":{"name":"pu","arguments":"{\"b\":\""}}]}}]}`,
		`data: {"id":"s","choices":[{"index":0,"delta":{"vendor":{"stage":1},"raw_field":["keep"],"tool_calls":[{"index":0,"id":"a","function":{"name":"re","arguments":"{\"a\":\""}}]}}]}`,
		`data: {"id":"s","choices":[{"index":0,"delta":{"content":"好","reasoning_content":"考","refusal":"可","vendor":"new","tool_calls":[{"index":1,"id":"-1","function":{"name":"blish","arguments":"你好\"}"}}]}}]}`,
		`data: {"id":"s","choices":[{"index":0,"delta":{"content":null,"tool_calls":[{"index":0,"id":"-0","function":{"name":"ad","arguments":"值\"}"}}]}}]}`,
		`data: {"id":"s","choices":[{"index":0,"delta":{"vendor":"-tail","empty":""},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":5}}`,
		`data: [DONE]`,
	}, "\n\n") + "\n\n"
	start := time.Unix(1, 0)
	tick := start
	raw, completion, timing, err := readChatStream(iotest.OneByteReader(strings.NewReader(body)), start, func() time.Time { tick = tick.Add(100 * time.Millisecond); return tick })
	if err != nil || string(raw) != body {
		t.Fatalf("stream/evidence: %v", err)
	}
	message := streamMessage(t, completion)
	for key, want := range map[string]string{"content": "你好", "reasoning_content": "思考", "refusal": "不可", "vendor": "new-tail", "empty": ""} {
		var got string
		if err := json.Unmarshal(message[key], &got); err != nil || got != want {
			t.Fatalf("%s=%s want %q: %v", key, message[key], want, err)
		}
	}
	var rawField []string
	if err := json.Unmarshal(message["raw_field"], &rawField); err != nil || !reflect.DeepEqual(rawField, []string{"keep"}) {
		t.Fatal(rawField, err)
	}
	var calls []wireCall
	if err := json.Unmarshal(message["tool_calls"], &calls); err != nil || len(calls) != 2 {
		t.Fatal(calls, err)
	}
	if calls[0].ID != "a-0" || calls[0].Function.Name != "read" || calls[0].Function.Arguments != `{"a":"值"}` || calls[1].ID != "b-1" || calls[1].Function.Name != "publish" || calls[1].Function.Arguments != `{"b":"你好"}` {
		t.Fatalf("interleaved calls: %+v", calls)
	}
	if timing.FirstTokenMs == nil || *timing.FirstTokenMs != 100 || timing.GenerationMs == nil || *timing.GenerationMs != 300 || timing.RequestMs != 500 {
		t.Fatalf("timing: %+v", timing)
	}
}

func TestStreamingAssemblyRejectsInvalidToolsAndPreservesFailureEvidence(t *testing.T) {
	for _, delta := range []string{
		`{"role":"user"}`, `{"tool_calls":"invalid"}`, `{"tool_calls":[{"index":-1}]}`,
		`{"tool_calls":[{"index":64}]}`, `{"tool_calls":[{"index":1}]}`, `{"tool_calls":[{"index":0,"type":"invalid"}]}`,
	} {
		body := `data: {"id":"s","choices":[{"index":0,"delta":` + delta + `,"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}` + "\n\ndata: [DONE]\n\n"
		raw, completion, _, err := readChatStream(strings.NewReader(body), time.Now(), time.Now)
		if err == nil || completion != nil || string(raw) != body {
			t.Fatalf("accepted malformed delta or lost evidence: %s %v", delta, err)
		}
	}
}

func TestStreamingLargeToolArgumentsSurviveReceiptCacheReplay(t *testing.T) {
	f := newContractFixture(t)
	c := f.Client.(*client)
	body, want := fragmentedStream(4096, "tools")
	requests := 0
	c.options.HTTPClient = &http.Client{Transport: responseTransport(func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	for range 2 {
		stream, err := c.Generate(t.Context(), f.Request)
		if err != nil {
			t.Fatal(err)
		}
		seen := 0
		for {
			delta, err := stream.Next(t.Context())
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if delta.Kind == ToolCallDelta {
				seen++
				var args struct{ Text string }
				if delta.ToolCall == nil || json.Unmarshal(delta.ToolCall.Arguments, &args) != nil || args.Text != want {
					t.Fatal("cached fragmented arguments differ")
				}
			}
		}
		stream.Close()
		if seen != 1 {
			t.Fatalf("tool calls=%d", seen)
		}
	}
	receipt, err := c.receipts.ByOperation(t.Context(), f.Owner.OwnerID, f.Request.Execution.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || !receipt.OutcomeKnown || receipt.Usage.InputTokens != 100 || receipt.Usage.OutputTokens != 100 || receipt.Charged == nil || receipt.Charged.Micros != 200 {
		t.Fatalf("request/usage evidence: %d %+v", requests, receipt)
	}
	cached, err := persistence.Load[cachedResult](t.Context(), c.store, "agent.model_requests", persistence.Key(f.Owner.OwnerID, f.Request.Execution.OperationID))
	if err != nil || cached.Response == nil || cached.Response.Body != body {
		t.Fatalf("raw evidence changed: %v", err)
	}
}
