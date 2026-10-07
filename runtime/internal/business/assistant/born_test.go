package assistant

import (
	"testing"

	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/tests/testkit"
)

func TestHireBirthPersistsWithCreationAndIsSchedulable(t *testing.T) {
	f := newContractFixture(t)
	a := hire(t, f)
	s := f.Service.(*service)
	events, err := persistence.Scan[event.AssistantEvent](t.Context(), s.store, "agent.events")
	testkit.RequireNoError(t, err)
	if len(events) != 1 {
		t.Fatalf("events: %+v", events)
	}
	born := events[0]
	if born.Kind != event.Born || born.AssistantID != a.ID || born.OwnerID != a.OwnerID || !born.CreatedAt.Equal(a.CreatedAt) || born.Delivery() != event.WhenIdle {
		t.Fatalf("birth source: %+v", born)
	}
	testkit.RequireNoError(t, born.Validate())
	claim, err := scheduler.NewQueue(s.store).Claim(t.Context(), a.CreatedAt)
	testkit.RequireNoError(t, err)
	if claim.Job.Event.ID != born.ID {
		t.Fatalf("claimed wrong event: %+v", claim)
	}
}

func TestBirthWriteFailureRollsBackHire(t *testing.T) {
	f := newContractFixture(t)
	a := hire(t, f)
	s := f.Service.(*service)
	events, err := persistence.Scan[event.AssistantEvent](t.Context(), s.store, "agent.events")
	testkit.RequireNoError(t, err)
	ids := []core.ID{"another-assistant", "another-conversation", events[0].ID}
	s.options.NewID = func() core.ID { id := ids[0]; ids = ids[1:]; return id }
	_, err = f.Service.Hire(t.Context(), owner(), HireRequest{
		Mutation: mutation("event-conflict", 0), Name: "Another", ModelID: a.ModelID, Budget: a.Budget,
	})
	testkit.RequireCode(t, err, core.Conflict)
	n, c, loops, births := f.Observe()
	if n != 1 || c != 1 || loops != 1 || births != 1 {
		t.Fatalf("partial hire after event write failure: %d %d %d %d", n, c, loops, births)
	}
}
