package agent

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
)

func contextBuildFixture() (*contextBuilder, Turn, privateContext) {
	builder := &contextBuilder{Options{Dependencies: Dependencies{Policy: fixtureAuthorizer{}}}}
	turn := Turn{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "execution"}, Event: event.AssistantEvent{ID: "event", OwnerID: "owner", AssistantID: "assistant", Kind: event.UserMessage, MessageID: "message", CreatedAt: time.Now()}}
	turn.Checkpoint = &Checkpoint{OwnerID: "owner", AssistantID: "assistant", FormatVersion: 1}
	state := privateContext{Inputs: []model.Input{{Role: model.System, Text: loopInstructions}, {Role: model.User, Text: "history"}}}
	return builder, turn, state
}

func TestDecodedContextBuildMatchesCheckpointAndDoesNotAppendToState(t *testing.T) {
	builder, turn, state := contextBuildFixture()
	// Spare capacity makes accidental append to the saved slice observable.
	state.Inputs = append(make([]model.Input, 0, 10), state.Inputs...)
	var err error
	turn.Checkpoint.Context, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := builder.Build(t.Context(), turn)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := builder.build(t.Context(), turn, &state)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(encoded, decoded) {
		t.Fatal("decoded builder changed model inputs")
	}
	if state.Inputs[:3][2].Role != "" {
		t.Fatal("builder mutated checkpoint backing array")
	}
	turn.Checkpoint.OwnerID = "foreign"
	if _, err := builder.build(t.Context(), turn, &state); core.CodeOf(err) != core.Forbidden {
		t.Fatal("decoded builder bypassed ownership", err)
	}
}

func BenchmarkContextBuildDecoded(b *testing.B) {
	builder, turn, state := contextBuildFixture()
	for range 256 {
		state.Inputs = append(state.Inputs, model.Input{Role: model.User, Text: strings.Repeat("context ", 1024)})
	}
	for _, decoded := range []bool{false, true} {
		name := "encoded-roundtrip"
		if decoded {
			name = "decoded"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if decoded {
					if _, err := builder.build(b.Context(), turn, &state); err != nil {
						b.Fatal(err)
					}
				} else {
					var err error
					turn.Checkpoint.Context, err = json.Marshal(state)
					if err != nil {
						b.Fatal(err)
					}
					if _, err := builder.Build(b.Context(), turn); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
