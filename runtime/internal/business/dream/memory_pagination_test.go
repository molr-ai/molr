package dream

import (
	"context"
	"strings"
	"testing"

	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/core"
)

type oversizedMemoryPages struct {
	memory.Service
	calls int
}

func (s *oversizedMemoryPages) List(_ context.Context, _ core.Principal, q memory.Query) (core.Page[memory.Memory], error) {
	s.calls++
	rows := make([]memory.Memory, 200)
	for i := range rows {
		rows[i] = memory.Memory{Text: strings.Repeat("x", 22<<10)}
	}
	return core.Page[memory.Memory]{Items: rows, NextCursor: "next"}, nil
}
func TestDreamBoundsMemoryAcrossPages(t *testing.T) {
	memories := &oversizedMemoryPages{}
	coordinator := &Coordinator{options: Options{Memories: memories}}
	_, err := coordinator.memories(t.Context(), core.Principal{OwnerID: "owner", SubjectID: "assistant"})
	if core.CodeOf(err) != core.FailedPrecondition || memories.calls != 2 {
		t.Fatal("paged content bypassed dream memory bound", err, memories.calls)
	}
}
