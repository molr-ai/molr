package assistant

import (
	"context"
	"molr.ai/runtime/internal/core"
	"testing"
)

func TestContextCompressionPreferenceLifecycle(t *testing.T) {
	f := newContractFixture(t)
	service := f.Service.(*service)
	window := 256 * 1024
	service.options.ContextWindow = func(context.Context, core.Principal, core.ID) (int, error) { return window, nil }
	a := hire(t, f)
	if a.ContextCompressionTokens != 0 {
		t.Fatal("default was materialized", a)
	}
	configured := 96 * 1024
	update := func(key string, value *int) {
		t.Helper()
		var err error
		a, err = service.Update(t.Context(), owner(), UpdateRequest{Mutation: mutation(key, a.Version), ID: a.ID, Name: a.Name, ModelID: a.ModelID, Budget: a.Budget, ContextCompressionTokens: value})
		if err != nil {
			t.Fatal(err)
		}
	}
	update("custom", &configured)
	update("preserve", nil)
	persisted, err := f.Repository.Get(t.Context(), owner().OwnerID, a.ID)
	if err != nil || persisted.ContextCompressionTokens != configured {
		t.Fatal(persisted, err)
	}
	clone, err := service.Clone(t.Context(), owner(), CloneRequest{Mutation: mutation("copy", 0), SourceID: a.ID, Name: "Copy"})
	if err != nil || clone.ContextCompressionTokens != configured {
		t.Fatal(clone, err)
	}
	window = 32 * 1024
	update("smaller-model", nil)
	if a.ContextCompressionTokens != 26214 {
		t.Fatal("manual threshold not clamped", a.ContextCompressionTokens)
	}
	automatic := 0
	update("reset", &automatic)
	window = 1024 * 1024
	update("automatic-follows-model", nil)
	if a.ContextCompressionTokens != 0 {
		t.Fatal("automatic preference lost")
	}
	negative := -1
	_, err = service.Update(t.Context(), owner(), UpdateRequest{Mutation: mutation("negative", a.Version), ID: a.ID, Name: a.Name, ModelID: a.ModelID, Budget: a.Budget, ContextCompressionTokens: &negative})
	wantCode(t, err, core.InvalidArgument)
}
