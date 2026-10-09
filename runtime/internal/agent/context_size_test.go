package agent

import (
	"encoding/json"
	"math"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/media"
	"reflect"
	"strings"
	"testing"
)

func TestContextSizeMatchesWholeJSONEncoding(t *testing.T) {
	timestamp := 1.2345
	cases := [][]model.Input{
		nil, {}, {{}},
		{{Role: model.User, Text: "Chinese 中文 😀 <>&\u2028\u2029\x00\n\t\"\\"}},
		{{Role: model.Tool, Text: string([]byte{'x', 0xff, 0xc0, 'y'}), FileIDs: []core.ID{}, Calls: []model.ToolCall{}}},
		{{Role: model.Assistant, ToolOperationID: "op", ReadMessageIDs: []core.ID{"message"}, RuntimeContext: true, Text: "answer", ProviderState: &model.ProviderState{Source: model.StateSource{Protocol: model.OpenAIChat, ResourceID: "resource", ModelID: "model", ModelName: "fixture", Endpoint: "https://example.invalid"}, Payload: json.RawMessage(` { "extension": "<>&", "reasoning": [null, 1, true] } `)}, Calls: []model.ToolCall{{ID: "call", Name: "read", Arguments: json.RawMessage(` { "path": "/tmp/<fixture>" } `)}}, Media: []media.Part{{FullAccess: true, SandboxID: "local", Kind: "image", MIME: "image/png", Snapshot: "digest", Scope: "assistant", ResourceID: "resource", Path: "/fixture", Version: "v1", Detail: "high", Timestamp: &timestamp, Data: []byte("never serialized")}}, FileIDs: []core.ID{"file"}, ReplyTo: "parent"}},
		{{Text: strings.Repeat("history\n", 1000)}, {Role: model.Tool, Text: "next"}, {Role: model.Assistant, Text: "last"}},
	}
	for i, inputs := range cases {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			encoded, err := json.Marshal(inputs)
			if err != nil {
				t.Fatal(err)
			}
			for _, limit := range []int{0, len(encoded) - 1, len(encoded), len(encoded) + 1} {
				err := checkContextSizeLimit(inputs, limit)
				if len(encoded) > limit {
					if core.CodeOf(err) != core.FailedPrecondition || core.ReasonOf(err) != core.ContextLimit {
						t.Fatal(len(encoded), limit, err)
					}
				} else if err != nil {
					t.Fatal(len(encoded), limit, err)
				}
			}
			after, err := json.Marshal(inputs)
			if err != nil || string(after) != string(encoded) {
				t.Fatal("check mutated context", err)
			}
		})
	}
}

func TestContextSizeKeepsEncodingErrorsEvenAfterLimit(t *testing.T) {
	badFloat := math.NaN()
	for _, invalid := range []model.Input{
		{Calls: []model.ToolCall{{Arguments: json.RawMessage(`{"bad":`)}}},
		{ProviderState: &model.ProviderState{Payload: json.RawMessage(`invalid`)}},
		{Media: []media.Part{{Timestamp: &badFloat}}},
	} {
		inputs := []model.Input{{Text: strings.Repeat("large", 1000)}, invalid}
		_, want := json.Marshal(inputs)
		if want == nil {
			t.Fatal("invalid fixture")
		}
		for _, limit := range []int{1, 1 << 20} {
			got := checkContextSizeLimit(inputs, limit)
			if got == nil || reflect.TypeOf(got) != reflect.TypeOf(want) || got.Error() != want.Error() {
				t.Fatal("encoding error changed", got, want)
			}
		}
	}
}

func TestContextSizeRetainsProductionLimit(t *testing.T) {
	// Share the source string so the test need not allocate a 256 MiB input copy.
	inputs := make([]model.Input, 256)
	text := strings.Repeat("a", 1<<20)
	for i := range inputs {
		inputs[i] = model.Input{Text: text}
	}
	if err := checkContextSize(inputs[:255]); err != nil {
		t.Fatal("rejected valid history", err)
	}
	if err := checkContextSize(inputs); core.ReasonOf(err) != core.ContextLimit {
		t.Fatal("accepted history above 256 MiB", err)
	}
}

func FuzzContextSizeMatchesJSON(f *testing.F) {
	f.Add("<fixture>\n中文", `{"value":[1,true,null]}`)
	f.Add(string([]byte{0xff, 0, 1}), `invalid`)
	f.Fuzz(func(t *testing.T, text, raw string) {
		if len(text) > 65536 || len(raw) > 65536 {
			return
		}
		inputs := []model.Input{{Text: text, Calls: []model.ToolCall{{Name: text, Arguments: json.RawMessage(raw)}}}, {Text: raw, RuntimeContext: true}}
		encoded, want := json.Marshal(inputs)
		if want != nil {
			got := checkContextSizeLimit(inputs, 1)
			if got == nil || core.ReasonOf(got) == core.ContextLimit {
				t.Fatal("invalid encoding accepted", got)
			}
			return
		}
		if err := checkContextSizeLimit(inputs, len(encoded)); err != nil {
			t.Fatal("exact limit rejected", err)
		}
		if err := checkContextSizeLimit(inputs, len(encoded)-1); core.ReasonOf(err) != core.ContextLimit {
			t.Fatal("oversized encoding accepted", err)
		}
	})
}
