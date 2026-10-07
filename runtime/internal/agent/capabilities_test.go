package agent

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
	"testing"
)

func TestCapabilitiesOwnerInspectionDoesNotStartExecution(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	engine := f.Runtime.(*Engine)
	called := false
	engine.options.InspectCapabilities = func(_ context.Context, p core.Principal, id core.ID) (Capabilities, error) {
		called = true
		if p.Kind != core.ActorOwner || id != f.Turn.Event.AssistantID {
			t.Fatal("inspection identity changed")
		}
		return Capabilities{AssistantID: id}, nil
	}
	result, err := engine.Capabilities(testkit.Context(t), f.Owner, f.Turn.Event.AssistantID)
	testkit.RequireNoError(t, err)
	if !called || result.AssistantID != f.Turn.Event.AssistantID || len(f.ModelRequests()) != 0 {
		t.Fatal("invalid inspection")
	}
	called = false
	if _, err := engine.Capabilities(testkit.Context(t), f.Turn.Principal, f.Turn.Event.AssistantID); err == nil || called {
		t.Fatal("assistant reached owner inspection")
	}
}
