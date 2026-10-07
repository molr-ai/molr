package event

import (
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
)

func TestBornValidatesLifecycleSource(t *testing.T) {
	source := AssistantEvent{ID: "born", OwnerID: "owner", AssistantID: "assistant", Kind: Born, CreatedAt: time.Now()}
	testkit.RequireNoError(t, source.Validate())
	for _, change := range []func(*AssistantEvent){
		func(e *AssistantEvent) { e.DeliveryMode = Interrupt },
		func(e *AssistantEvent) { e.DeliveryMode = AfterStep },
		func(e *AssistantEvent) { e.MessageID = "message" },
		func(e *AssistantEvent) { e.MessageSequence = 1 },
		func(e *AssistantEvent) { e.ScheduleID = "schedule" },
		func(e *AssistantEvent) { e.ScheduleVersion = 1 },
		func(e *AssistantEvent) { e.OperationID = "operation" },
		func(e *AssistantEvent) { e.TaskID = "task" },
		func(e *AssistantEvent) { e.ExecutionID = "execution" },
		func(e *AssistantEvent) { e.OccurrenceAt = time.Now() },
		func(e *AssistantEvent) { e.Manual = true },
	} {
		invalid := source
		change(&invalid)
		testkit.RequireCode(t, invalid.Validate(), core.InvalidArgument)
	}
}

func TestBornDeduplicatesPerOwnerAndAssistantAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "born.db")
	db, err := sqlitestore.Open(path)
	testkit.RequireNoError(t, err)
	source := AssistantEvent{ID: "first", OwnerID: "owner", AssistantID: "assistant", Kind: Born, CreatedAt: time.Now()}
	testkit.RequireNoError(t, NewWriter(db).AppendAssistant(t.Context(), source))
	testkit.RequireNoError(t, db.Close())
	db, err = sqlitestore.Open(path)
	testkit.RequireNoError(t, err)
	defer db.Close()
	writer := NewWriter(db)
	source.ID = "replayed"
	source.CreatedAt = source.CreatedAt.Add(time.Hour)
	testkit.RequireNoError(t, writer.AppendAssistant(t.Context(), source))
	source.AssistantID = "another"
	testkit.RequireNoError(t, writer.AppendAssistant(t.Context(), source))
	source.OwnerID = "other"
	testkit.RequireNoError(t, writer.AppendAssistant(t.Context(), source))
	rows, err := persistence.Scan[AssistantEvent](t.Context(), db, eventsBucket)
	testkit.RequireNoError(t, err)
	if len(rows) != 3 {
		t.Fatalf("birth events = %+v", rows)
	}
}
