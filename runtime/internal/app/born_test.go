package app

import (
	"testing"

	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/event"
)

// Post-creation fixtures explicitly cancel birth so their model scripts and
// checkpoints belong only to the later event being tested.
func cancelPendingBirth(t *testing.T, b *Bundle, a assistant.Assistant) {
	t.Helper()
	queue := scheduler.NewQueue(b.store)
	jobs, err := queue.List(t.Context(), a.OwnerID, scheduler.Query{AssistantID: a.ID})
	need(t, err)
	found := false
	for _, job := range jobs.Items {
		if job.Event.Kind == event.Born {
			found = true
			need(t, queue.Cancel(t.Context(), a.OwnerID, job.Event.ID))
		}
	}
	if !found {
		t.Fatal("hire did not enqueue birth")
	}
}
